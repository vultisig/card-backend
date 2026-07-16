// Package push fans webhook-driven events out to all enrolled devices of a
// vault (transactions, deposits, 3DS challenges, wallet-tokenization OTPs)
// via APNs and FCM. Devices are stateless readers; push carries references,
// never card data.
package push
