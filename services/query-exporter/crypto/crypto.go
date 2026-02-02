// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

type Crypto interface {
	Encrypt(text []byte) (string, error)
	Decrypt(text []byte) (string, error)
}

type aesCrypto struct {
	gcm cipher.AEAD
}

func NewCrypto(key []byte) (Crypto, error) {
	c, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, err
	}
	return aesCrypto{gcm: gcm}, nil
}

func (ac aesCrypto) Encrypt(text []byte) (string, error) {
	nonce := make([]byte, ac.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return string(ac.gcm.Seal(nonce, nonce, text, nil)), nil
}

func (ac aesCrypto) Decrypt(text []byte) (string, error) {
	nonceSize := ac.gcm.NonceSize()
	if len(text) < nonceSize {
		return "", fmt.Errorf("crypto err: text (%v) len is less than nonce len (%v)", text, nonceSize)
	}
	nonce, text := text[:nonceSize], text[nonceSize:]
	plaintext, err := ac.gcm.Open(nil, nonce, text, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
