package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

func Hash(password string, params Params) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", errors.New("password must contain 12..256 bytes")
	}
	if err := validateParams(params); err != nil {
		return "", err
	}
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.Memory, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func Verify(password, encoded string, target Params) (match, needsRehash bool) {
	params, salt, expected, err := parseHash(encoded)
	if err != nil {
		return false, false
	}
	actual := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, uint32(len(expected)))
	match = subtle.ConstantTimeCompare(actual, expected) == 1
	if !match {
		return false, false
	}
	return true, params != target
}

func parseHash(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, errors.New("invalid password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, errors.New("invalid password hash version")
	}
	params := Params{}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.Memory, &params.Iterations, &params.Parallelism); err != nil {
		return Params{}, nil, nil, errors.New("invalid password hash parameters")
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, errors.New("invalid password hash salt")
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, errors.New("invalid password hash key")
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(expected))
	if err := validateParams(params); err != nil {
		return Params{}, nil, nil, err
	}
	return params, salt, expected, nil
}

func validateParams(params Params) error {
	if params.Memory < 8*1024 || params.Memory > 1024*1024 || params.Iterations < 1 || params.Iterations > 10 ||
		params.Parallelism < 1 || params.Parallelism > 32 || params.SaltLength < 8 || params.SaltLength > 64 ||
		params.KeyLength < 16 || params.KeyLength > 64 {
		return errors.New("invalid Argon2id parameters")
	}
	return nil
}
