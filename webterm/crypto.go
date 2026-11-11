package webterm

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
)

// obfuscatedKey is the key the terminal ships with. The bundle stores it
// with every character shifted up by one so it is not readable as hex.
const obfuscatedKey = "13ef13b2b76dd8:5795gdcfb2fdc1ge85bf768f54773d22fff996e3ge75g5:75"

// deobfuscate reverses the shift: every code point is one lower, except
// 28 and 23 which stand for '&' and '!'.
func deobfuscate(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case 28:
			out = append(out, '&')
		case 23:
			out = append(out, '!')
		default:
			out = append(out, r-1)
		}
	}
	return string(out)
}

// initialKey returns the key the session starts with, before the server
// hands out a session key.
func initialKey() ([]byte, error) {
	key, err := hex.DecodeString(deobfuscate(obfuscatedKey))
	if err != nil {
		return nil, fmt.Errorf("webterm: bad initial key: %w", err)
	}
	return key, nil
}

// aesCipher is AES-CBC with an all zero IV and PKCS#7 padding, which is what
// the terminal's Web Crypto calls do.
type aesCipher struct {
	block cipher.Block
}

func newAES(key []byte) (*aesCipher, error) {
	switch len(key) {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("webterm: invalid AES key length %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &aesCipher{block: block}, nil
}

func (c *aesCipher) encrypt(data []byte) ([]byte, error) {
	size := c.block.BlockSize()
	padded := pkcs7Pad(data, size)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(c.block, make([]byte, size)).CryptBlocks(out, padded)
	return out, nil
}

func (c *aesCipher) decrypt(data []byte) ([]byte, error) {
	size := c.block.BlockSize()
	if len(data) == 0 || len(data)%size != 0 {
		return nil, fmt.Errorf("webterm: ciphertext length %d is not a multiple of %d", len(data), size)
	}
	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(c.block, make([]byte, size)).CryptBlocks(out, data)
	return pkcs7Unpad(out, size)
}

// pkcs7Pad appends PKCS#7 padding to data.
func pkcs7Pad(data []byte, size int) []byte {
	n := size - len(data)%size
	out := make([]byte, len(data)+n)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(n)
	}
	return out
}

// pkcs7Unpad removes PKCS#7 padding from data.
func pkcs7Unpad(data []byte, size int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("webterm: empty plaintext")
	}
	n := int(data[len(data)-1])
	if n == 0 || n > size || n > len(data) {
		return nil, fmt.Errorf("webterm: bad padding length %d", n)
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, fmt.Errorf("webterm: bad padding")
		}
	}
	return data[:len(data)-n], nil
}
