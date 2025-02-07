package hdwallet

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Key struct {
	PrivateKey []byte
	ChainCode  []byte
}

func CKDPriv(key Key, index uint32) Key {
	buffer := make([]byte, 0, 1+4+len(key.PrivateKey))
	buffer = append(buffer, 0)
	buffer = append(buffer, key.PrivateKey...)
	v := make([]byte, 4)
	binary.BigEndian.PutUint32(v, index)
	buffer = append(buffer, v...)

	I := hamc512(key.ChainCode, buffer)

	return Key{
		PrivateKey: I[:32],
		ChainCode:  I[32:],
	}
}

func CreateMasterKey(seed []byte) Key {
	I := hamc512([]byte("ed25519 seed"), seed)
	return Key{
		PrivateKey: I[:32],
		ChainCode:  I[32:],
	}
}

func hamc512(key, data []byte) []byte {
	hmac := hmac.New(sha512.New, key)
	hmac.Write(data)
	return hmac.Sum(nil)
}

func isValidPath(path string) bool {
	r := regexp.MustCompile(`^m{1,1}(/\d+')*$`)
	return r.MatchString(path)
}

func Derived(path string, seed []byte) (Key, error) {
	if !isValidPath(path) {
		return Key{}, fmt.Errorf("invalid path: %v", path)
	}

	key := CreateMasterKey(seed)

	for _, s := range strings.Split(path, "/")[1:] {
		v, err := strconv.ParseUint(s[:len(s)-1], 10, 32)
		if err != nil {
			return Key{}, fmt.Errorf("failed to parse %v as a uint, err: %v", v, err)
		}

		key = CKDPriv(key, uint32(v)+1<<31)
	}

	return key, nil
}
