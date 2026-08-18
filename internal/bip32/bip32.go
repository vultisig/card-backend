// Package bip32 implements unhardened-only BIP32 key derivation over
// secp256k1. Vultisig's TSS key scheme has no notion of a hardened private
// key, so hardened derivation (index >= HardenedOffset) is intentionally
// unsupported.
package bip32

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"errors"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// HardenedOffset is the first index in BIP32's hardened range.
const HardenedOffset = uint32(0x80000000)

// ETHPath is m/44/60/0/0/0, unhardened at every level.
var ETHPath = []uint32{44, 60, 0, 0, 0}

var errHardened = errors.New("bip32: hardened derivation not supported")

// ExtendedPrivateKey is a BIP32 extended private key.
type ExtendedPrivateKey struct {
	Priv      *secp256k1.PrivateKey
	ChainCode [32]byte
}

// MasterFromSeed derives the BIP32 master extended private key from a BIP39
// seed.
func MasterFromSeed(seed []byte) *ExtendedPrivateKey {
	mac := hmac.New(sha512.New, []byte("Bitcoin seed"))
	mac.Write(seed)
	sum := mac.Sum(nil)
	k := &ExtendedPrivateKey{Priv: secp256k1.PrivKeyFromBytes(sum[:32])}
	copy(k.ChainCode[:], sum[32:])
	return k
}

// Child derives the CKDpriv child at index.
func (k *ExtendedPrivateKey) Child(index uint32) (*ExtendedPrivateKey, error) {
	if index >= HardenedOffset {
		return nil, errHardened
	}
	data := k.Priv.PubKey().SerializeCompressed()
	data = binary.BigEndian.AppendUint32(data, index)

	mac := hmac.New(sha512.New, k.ChainCode[:])
	mac.Write(data)
	sum := mac.Sum(nil)

	var il secp256k1.ModNScalar
	if overflow := il.SetByteSlice(sum[:32]); overflow || il.IsZero() {
		return nil, errors.New("bip32: invalid derived key, retry with a different seed")
	}
	il.Add(&k.Priv.Key)
	if il.IsZero() {
		return nil, errors.New("bip32: invalid derived key, retry with a different seed")
	}

	child := &ExtendedPrivateKey{Priv: secp256k1.NewPrivateKey(&il)}
	copy(child.ChainCode[:], sum[32:])
	return child, nil
}

// DerivePath walks path from k via successive unhardened Child derivations.
func (k *ExtendedPrivateKey) DerivePath(path []uint32) (*ExtendedPrivateKey, error) {
	cur := k
	for _, index := range path {
		next, err := cur.Child(index)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

// ExtendedPublicKey is a BIP32 extended public key. Chain codes aren't
// secret (this is what lets BIP32 watch-only wallets work), so an
// unhardened child's public key can be derived without its private key.
type ExtendedPublicKey struct {
	Pub       *secp256k1.PublicKey
	ChainCode [32]byte
}

// Child derives the CKDpub child at index.
func (k *ExtendedPublicKey) Child(index uint32) (*ExtendedPublicKey, error) {
	if index >= HardenedOffset {
		return nil, errHardened
	}
	data := k.Pub.SerializeCompressed()
	data = binary.BigEndian.AppendUint32(data, index)

	mac := hmac.New(sha512.New, k.ChainCode[:])
	mac.Write(data)
	sum := mac.Sum(nil)

	var il secp256k1.ModNScalar
	if overflow := il.SetByteSlice(sum[:32]); overflow || il.IsZero() {
		return nil, errors.New("bip32: invalid derived key, retry with a different chain code")
	}

	var ilPoint, parentPoint, childPoint secp256k1.JacobianPoint
	secp256k1.ScalarBaseMultNonConst(&il, &ilPoint)
	k.Pub.AsJacobian(&parentPoint)
	secp256k1.AddNonConst(&ilPoint, &parentPoint, &childPoint)
	childPoint.ToAffine()

	child := &ExtendedPublicKey{Pub: secp256k1.NewPublicKey(&childPoint.X, &childPoint.Y)}
	copy(child.ChainCode[:], sum[32:])
	return child, nil
}

// DerivePath walks path from k via successive unhardened Child derivations.
func (k *ExtendedPublicKey) DerivePath(path []uint32) (*ExtendedPublicKey, error) {
	cur := k
	for _, index := range path {
		next, err := cur.Child(index)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}
