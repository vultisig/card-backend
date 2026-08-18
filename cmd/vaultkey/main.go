// Command vaultkey generates a BIP39 seed phrase and derives:
//   - a root secp256k1 keypair, and an ETH keypair from it via unhardened
//     BIP32 derivation (m/44/60/0/0/0 — every level unhardened, since
//     hardened derivation isn't supported by Vultisig's TSS-based key
//     scheme)
//   - a Solana (ed25519) keypair via SLIP-0010 hardened derivation
//     (m/44'/501'/0'/0')
//
// It can also sign a POST /auth nonce challenge with the ETH key, and/or
// sign an arbitrary JSON message with both the ETH and Solana keys — e.g.
// for REAP's account Signers.evm/Signers.svm fields.
package main

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/sha3"

	"github.com/vultisig/card-backend/internal/bip32"
)

// ethAddress derives the EIP-55 checksummed Ethereum address for pub:
// the last 20 bytes of Keccak-256(uncompressed pubkey without its 0x04
// prefix), with each hex digit's case set per EIP-55.
func ethAddress(pub *secp256k1.PublicKey) string {
	keccak := sha3.NewLegacyKeccak256()
	keccak.Write(pub.SerializeUncompressed()[1:])
	addr := keccak.Sum(nil)[12:]
	addrHex := hex.EncodeToString(addr)

	keccak.Reset()
	keccak.Write([]byte(addrHex))
	hashHex := hex.EncodeToString(keccak.Sum(nil))

	checksummed := make([]byte, len(addrHex))
	for i, c := range []byte(addrHex) {
		if c >= 'a' && c <= 'f' && hashHex[i] >= '8' {
			checksummed[i] = c - ('a' - 'A')
		} else {
			checksummed[i] = c
		}
	}
	return "0x" + string(checksummed)
}

// solanaExtendedKey is a SLIP-0010 ed25519 extended key. Unlike secp256k1,
// ed25519 has no public-parent-key-to-public-child-key derivation, so SLIP-10
// only defines (and this only implements) hardened derivation.
type solanaExtendedKey struct {
	seed      [32]byte // fed directly to ed25519.NewKeyFromSeed, per SLIP-0010
	chainCode [32]byte
}

// solanaPath is m/44'/501'/0'/0', the standard Solana derivation path (used
// by Phantom and most other Solana wallets). Every level is hardened.
var solanaPath = []uint32{44, 501, 0, 0}

const solanaHardenedOffset = uint32(0x80000000)

func solanaMasterFromSeed(seed []byte) *solanaExtendedKey {
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(seed)
	sum := mac.Sum(nil)
	k := &solanaExtendedKey{}
	copy(k.seed[:], sum[:32])
	copy(k.chainCode[:], sum[32:])
	return k
}

func (k *solanaExtendedKey) hardenedChild(index uint32) *solanaExtendedKey {
	data := make([]byte, 0, 1+32+4)
	data = append(data, 0x00)
	data = append(data, k.seed[:]...)
	data = binary.BigEndian.AppendUint32(data, index|solanaHardenedOffset)

	mac := hmac.New(sha512.New, k.chainCode[:])
	mac.Write(data)
	sum := mac.Sum(nil)

	child := &solanaExtendedKey{}
	copy(child.seed[:], sum[:32])
	copy(child.chainCode[:], sum[32:])
	return child
}

func (k *solanaExtendedKey) derivePath(path []uint32) *solanaExtendedKey {
	cur := k
	for _, index := range path {
		cur = cur.hardenedChild(index)
	}
	return cur
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Encode is the Bitcoin-alphabet base58 encoding Solana uses for
// addresses and signatures.
func base58Encode(input []byte) string {
	x := new(big.Int).SetBytes(input)
	base := big.NewInt(58)
	mod := new(big.Int)
	var out []byte
	for x.Sign() > 0 {
		x.DivMod(x, base, mod)
		out = append(out, base58Alphabet[mod.Int64()])
	}
	for _, b := range input {
		if b != 0 {
			break
		}
		out = append(out, base58Alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

func main() {
	mnemonicFile := flag.String("mnemonic-file", "", "path to a file containing an existing BIP39 mnemonic (default: generate a new one)")
	passphrase := flag.String("passphrase", "", "optional BIP39 passphrase")
	nonce := flag.Int64("nonce", -1, "nonce to sign for POST /auth (matches the server's sha256(nonce) challenge); omit to skip signing")
	message := flag.String("message", "", "inline JSON message to sign with the derived ETH private key")
	messageFile := flag.String("message-file", "", "path to a file containing a JSON message to sign with the derived ETH private key")
	flag.Parse()

	var mnemonic string
	if *mnemonicFile != "" {
		data, err := os.ReadFile(*mnemonicFile)
		if err != nil {
			log.Fatalf("read mnemonic file: %v", err)
		}
		mnemonic = strings.TrimSpace(string(data))
	}
	if mnemonic == "" {
		entropy, err := bip39.NewEntropy(128)
		if err != nil {
			log.Fatalf("generate entropy: %v", err)
		}
		mnemonic, err = bip39.NewMnemonic(entropy)
		if err != nil {
			log.Fatalf("generate mnemonic: %v", err)
		}
	} else if !bip39.IsMnemonicValid(mnemonic) {
		log.Fatal("invalid mnemonic")
	}

	root := bip32.MasterFromSeed(bip39.NewSeed(mnemonic, *passphrase))
	eth, err := root.DerivePath(bip32.ETHPath)
	if err != nil {
		log.Fatalf("derive eth key: %v", err)
	}

	solana := solanaMasterFromSeed(bip39.NewSeed(mnemonic, *passphrase)).derivePath(solanaPath)
	solanaPriv := ed25519.NewKeyFromSeed(solana.seed[:])
	solanaPub := solanaPriv.Public().(ed25519.PublicKey)
	solanaAddress := base58Encode(solanaPub)

	rootChainCodeHex := hex.EncodeToString(root.ChainCode[:])
	ethPubHex := hex.EncodeToString(eth.Priv.PubKey().SerializeCompressed())

	field := func(label, value string) { fmt.Printf("%-20s%s\n", label+":", value) }

	field("Mnemonic", mnemonic)
	field("Root Private Key", hex.EncodeToString(root.Priv.Serialize()))
	field("Root Public Key", hex.EncodeToString(root.Priv.PubKey().SerializeCompressed()))
	field("Root Chain Code", rootChainCodeHex)
	field("ETH Path", "m/44/60/0/0/0 (unhardened)")
	field("ETH Private Key", hex.EncodeToString(eth.Priv.Serialize()))
	field("ETH Public Key", ethPubHex)
	field("ETH Address", ethAddress(eth.Priv.PubKey()))
	fmt.Println()
	field("Solana Path", "m/44'/501'/0'/0' (hardened, SLIP-0010)")
	field("Solana Private Key", hex.EncodeToString(solana.seed[:]))
	field("Solana Address", solanaAddress)

	if *nonce >= 0 {
		hash := sha256.Sum256([]byte(strconv.FormatInt(*nonce, 10)))
		sigHex := hex.EncodeToString(ecdsa.Sign(eth.Priv, hash[:]).Serialize())
		fmt.Println()
		field("Nonce", strconv.FormatInt(*nonce, 10))
		field("ETH Signature (DER)", sigHex)
		fmt.Println()
		fmt.Printf("POST /auth body:   {\"public_key\":%q,\"chain_code\":%q,\"nonce\":%d,\"signature\":%q}\n",
			hex.EncodeToString(root.Priv.PubKey().SerializeCompressed()), rootChainCodeHex, *nonce, sigHex)
	}

	msg := *message
	if *messageFile != "" {
		data, err := os.ReadFile(*messageFile)
		if err != nil {
			log.Fatalf("read message file: %v", err)
		}
		msg = string(data)
	}
	if msg != "" {
		if !json.Valid([]byte(msg)) {
			log.Fatal("message is not valid JSON")
		}
		hash := sha256.Sum256([]byte(msg))
		ethSigHex := hex.EncodeToString(ecdsa.Sign(eth.Priv, hash[:]).Serialize())
		solanaSig := base58Encode(ed25519.Sign(solanaPriv, []byte(msg)))
		fmt.Println()
		field("Message", msg)
		field("Message SHA-256", hex.EncodeToString(hash[:]))
		field("ETH Signature (DER)", ethSigHex)
		field("Solana Signature", solanaSig)
	}
}
