package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := Hash("correct horse battery staple", DefaultParams)
	require.NoError(t, err)
	require.Regexp(t, `^\$argon2id\$v=19\$m=65536,t=3,p=2\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+$`, encoded)

	match, needsRehash := Verify("correct horse battery staple", encoded, DefaultParams)
	require.True(t, match)
	require.False(t, needsRehash)

	match, needsRehash = Verify("incorrect password", encoded, DefaultParams)
	require.False(t, match)
	require.False(t, needsRehash)
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, encoded := range []string{"", "not-a-hash", "$argon2i$v=19$m=65536,t=3,p=2$bad$bad", "$argon2id$v=18$m=65536,t=3,p=2$bad$bad"} {
		match, needsRehash := Verify("correct horse battery staple", encoded, DefaultParams)
		require.False(t, match, encoded)
		require.False(t, needsRehash, encoded)
	}
}

func TestVerifyRequestsRehashForWeakerParameters(t *testing.T) {
	weak := DefaultParams
	weak.Memory /= 2
	encoded, err := Hash("correct horse battery staple", weak)
	require.NoError(t, err)

	match, needsRehash := Verify("correct horse battery staple", encoded, DefaultParams)
	require.True(t, match)
	require.True(t, needsRehash)
}

func TestHashRejectsPasswordLengthOutsideBounds(t *testing.T) {
	_, err := Hash("too-short", DefaultParams)
	require.ErrorContains(t, err, "12")

	_, err = Hash(strings.Repeat("x", 257), DefaultParams)
	require.ErrorContains(t, err, "256")
}
