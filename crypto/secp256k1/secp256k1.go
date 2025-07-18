package secp256k1

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/ripemd160" //nolint: gosec,staticcheck // necessary for Bitcoin address format

	"github.com/cometbft/cometbft/v2/crypto"
	cmtjson "github.com/cometbft/cometbft/v2/libs/json"
)

// -------------------------------------.
const (
	PrivKeyName = "tendermint/PrivKeySecp256k1"
	PubKeyName  = "tendermint/PubKeySecp256k1"

	KeyType     = "secp256k1"
	PrivKeySize = 32
)

func init() {
	cmtjson.RegisterType(PubKey{}, PubKeyName)
	cmtjson.RegisterType(PrivKey{}, PrivKeyName)
}

var _ crypto.PrivKey = PrivKey{}

// PrivKey implements PrivKey.
type PrivKey []byte

// Bytes returns the privkey as bytes.
func (privKey PrivKey) Bytes() []byte {
	return []byte(privKey)
}

// PubKey performs the point-scalar multiplication from the privKey on the
// generator point to get the pubkey.
//
// See secp256k1.PrivKeyFromBytes.
func (privKey PrivKey) PubKey() crypto.PubKey {
	secpPrivKey := secp256k1.PrivKeyFromBytes(privKey)

	pk := secpPrivKey.PubKey().SerializeCompressed()

	return PubKey(pk)
}

// Type returns the key type.
func (PrivKey) Type() string {
	return KeyType
}

// GenPrivKey generates a new ECDSA private key on curve secp256k1 private key.
// It uses OS randomness to generate the private key.
//
// See crypto.CReader.
func GenPrivKey() PrivKey {
	return genPrivKey(crypto.CReader())
}

func genPrivKey(rand io.Reader) PrivKey {
	var privKeyBytes [PrivKeySize]byte
	d := new(big.Int)

	for {
		privKeyBytes = [PrivKeySize]byte{}
		_, err := io.ReadFull(rand, privKeyBytes[:])
		if err != nil {
			panic(err)
		}

		d.SetBytes(privKeyBytes[:])
		// break if we found a valid point (i.e. > 0 and < N == curveOrder)
		isValidFieldElement := 0 < d.Sign() && d.Cmp(secp256k1.S256().N) < 0
		if isValidFieldElement {
			break
		}
	}

	// crypto.CRandBytes is guaranteed to be 32 bytes long, so it can be
	// cast to PrivKey.
	return PrivKey(privKeyBytes[:])
}

var one = new(big.Int).SetInt64(1)

// GenPrivKeySecp256k1 hashes the secret with SHA2, and uses
// that 32 byte output to create the private key.
//
// It makes sure the private key is a valid field element by setting:
//
// c = sha256(secret)
// k = (c mod (n − 1)) + 1, where n = curve order.
//
// NOTE: secret should be the output of a KDF like bcrypt,
// if it's derived from user input.
func GenPrivKeySecp256k1(secret []byte) PrivKey {
	secHash := sha256.Sum256(secret)
	// to guarantee that we have a valid field element, we use the approach of:
	// "Suite B Implementer’s Guide to FIPS 186-3", A.2.1
	// https://apps.nsa.gov/iaarchive/library/ia-guidance/ia-solutions-for-classified/algorithm-guidance/suite-b-implementers-guide-to-fips-186-3-ecdsa.cfm
	// see also https://github.com/golang/go/blob/0380c9ad38843d523d9c9804fe300cb7edd7cd3c/src/crypto/ecdsa/ecdsa.go#L89-L101
	fe := new(big.Int).SetBytes(secHash[:])
	n := new(big.Int).Sub(secp256k1.S256().N, one)
	fe.Mod(fe, n)
	fe.Add(fe, one)

	feB := fe.Bytes()
	privKey32 := make([]byte, PrivKeySize)
	// copy feB over to fixed 32 byte privKey32 and pad (if necessary)
	copy(privKey32[32-len(feB):32], feB)

	return PrivKey(privKey32)
}

// Sign creates an ECDSA signature on curve Secp256k1, using SHA256 on the msg.
// The returned signature will be of the form R || S (in lower-S form).
func (privKey PrivKey) Sign(msg []byte) ([]byte, error) {
	priv := secp256k1.PrivKeyFromBytes(privKey)

	sum := sha256.Sum256(msg)
	sig := ecdsa.SignCompact(priv, sum[:], false)

	// remove the first byte which is compactSigRecoveryCode
	return sig[1:], nil
}

// -------------------------------------

var _ crypto.PubKey = PubKey{}

// PubKeySize is comprised of 32 bytes for one field element
// (the x-coordinate), plus one byte for the parity of the y-coordinate.
const PubKeySize = 33

// PubKey implements crypto.PubKey.
// It is the compressed form of the pubkey. The first byte depends is a 0x02 byte
// if the y-coordinate is the lexicographically largest of the two associated with
// the x-coordinate. Otherwise the first byte is a 0x03.
// This prefix is followed with the x-coordinate.
type PubKey []byte

// Address returns a Bitcoin style address: RIPEMD160(SHA256(pubkey)).
func (pubKey PubKey) Address() crypto.Address {
	if len(pubKey) != PubKeySize {
		panic(fmt.Sprintf("length of pubkey is incorrect %d != %d", len(pubKey), PubKeySize))
	}
	hasherSHA256 := sha256.New()
	_, err := hasherSHA256.Write(pubKey)
	if err != nil {
		panic(err)
	}
	sha := hasherSHA256.Sum(nil)

	// Check if the size of the hash is what we expect.
	if ripemd160.Size != crypto.AddressSize {
		panic("ripemd160.Size != crypto.AddressSize")
	}

	hasherRIPEMD160 := ripemd160.New() // #nosec G406 // necessary for Bitcoin address format
	_, err = hasherRIPEMD160.Write(sha)
	if err != nil {
		panic(err)
	}

	return crypto.Address(hasherRIPEMD160.Sum(nil))
}

// Bytes returns the pubkey as bytes.
func (pubKey PubKey) Bytes() []byte {
	return []byte(pubKey)
}

func (pubKey PubKey) String() string {
	return fmt.Sprintf("PubKeySecp256k1{%X}", []byte(pubKey))
}

// Type returns the key type.
func (PubKey) Type() string {
	return KeyType
}

// VerifySignature verifies a signature of the form R || S.
// It rejects signatures which are not in lower-S form.
func (pubKey PubKey) VerifySignature(msg []byte, sigStr []byte) bool {
	if len(sigStr) != 64 {
		return false
	}

	pub, err := secp256k1.ParsePubKey(pubKey)
	if err != nil {
		return false
	}

	// parse the signature:
	signature := signatureFromBytes(sigStr)
	// Reject malleable signatures. libsecp256k1 does this check but decred doesn't.
	// see: https://github.com/ethereum/go-ethereum/blob/f9401ae011ddf7f8d2d95020b7446c17f8d98dc1/crypto/signature_nocgo.go#L90-L93
	// Serialize() would negate S value if it is over half order.
	// Hence, if the signature is different after Serialize() if should be rejected.
	modifiedSignature, parseErr := ecdsa.ParseDERSignature(signature.Serialize())
	if parseErr != nil {
		return false
	}
	if !signature.IsEqual(modifiedSignature) {
		return false
	}

	sum := sha256.Sum256(msg)
	return signature.Verify(sum[:], pub)
}

// Read Signature struct from R || S. Caller needs to ensure
// that len(sigStr) == 64.
func signatureFromBytes(sigStr []byte) *ecdsa.Signature {
	var r secp256k1.ModNScalar
	r.SetByteSlice(sigStr[:32])
	var s secp256k1.ModNScalar
	s.SetByteSlice(sigStr[32:64])
	return ecdsa.NewSignature(&r, &s)
}
