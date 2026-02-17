package secrets

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("read error")
}

func fixedKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func withNewGCM(t *testing.T, fn func(cipher.Block) (cipher.AEAD, error)) {
	t.Helper()
	old := newGCM
	newGCM = fn
	t.Cleanup(func() {
		newGCM = old
	})
}

func TestParseKey_Raw32(t *testing.T) {
	raw := strings.Repeat("a", 32)
	key, err := ParseKey(raw)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if string(key) != raw {
		t.Fatalf("expected raw key to match, got %q", string(key))
	}
}

func TestParseKey_Base64Valid(t *testing.T) {
	input := fixedKey()
	encoded := base64.StdEncoding.EncodeToString(input)
	key, err := ParseKey(encoded)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !bytes.Equal(key, input) {
		t.Fatalf("expected decoded key to match")
	}
}

func TestParseKey_Base64Invalid(t *testing.T) {
	_, err := ParseKey("not-base64!!")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseKey_WrongLength(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := ParseKey(encoded)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseKey_Empty(t *testing.T) {
	_, err := ParseKey("")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := fixedKey()
	plaintext := "hello"
	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	result, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != plaintext {
		t.Fatalf("expected %q, got %q", plaintext, result)
	}
}

func TestEncryptDecrypt_DifferentPlaintexts(t *testing.T) {
	key := fixedKey()
	inputs := []string{"", "short", "with spaces", "line1\nline2"}
	for _, input := range inputs {
		ciphertext, err := Encrypt(key, input)
		if err != nil {
			t.Fatalf("expected no error for %q, got %v", input, err)
		}
		result, err := Decrypt(key, ciphertext)
		if err != nil {
			t.Fatalf("expected no error for %q, got %v", input, err)
		}
		if result != input {
			t.Fatalf("expected %q, got %q", input, result)
		}
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key := fixedKey()
	wrongKey := make([]byte, 32)
	copy(wrongKey, key)
	wrongKey[0] ^= 0xff
	encoded, err := Encrypt(key, "secret")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_, err = Decrypt(wrongKey, encoded)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecrypt_Tampered(t *testing.T) {
	key := fixedKey()
	encoded, err := Encrypt(key, "secret")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	data[len(data)-1] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(data)
	_, err = Decrypt(key, tampered)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	_, err := Decrypt(fixedKey(), "%%%")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecrypt_ShortCiphertext(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("short"))
	_, err := Decrypt(fixedKey(), encoded)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncrypt_InvalidKey(t *testing.T) {
	_, err := Encrypt([]byte("short"), "data")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecrypt_InvalidKey(t *testing.T) {
	_, err := Decrypt([]byte("short"), "data")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncrypt_RandError(t *testing.T) {
	key := fixedKey()
	oldReader := rand.Reader
	rand.Reader = errorReader{}
	t.Cleanup(func() {
		rand.Reader = oldReader
	})
	_, err := Encrypt(key, "data")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEncrypt_NewGCMError(t *testing.T) {
	key := fixedKey()
	withNewGCM(t, func(cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("gcm error")
	})
	_, err := Encrypt(key, "data")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecrypt_NewGCMError(t *testing.T) {
	key := fixedKey()
	withNewGCM(t, func(cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("gcm error")
	})
	_, err := Decrypt(key, "data")
	if err == nil {
		t.Fatal("expected error")
	}
}
