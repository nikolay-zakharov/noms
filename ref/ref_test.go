package ref

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseError(t *testing.T) {
	assert := assert.New(t)

	r, err := Parse("foo")
	assert.Error(err)
	r, err = Parse("sha1")
	assert.Error(err)
	r, err = Parse("sha1-0")
	assert.Error(err)

	// too many digits
	r, err = Parse("sha1-00000000000000000000000000000000000000000")
	assert.Error(err)

	// 'g' not valid hex
	r, err = Parse("sha1-	000000000000000000000000000000000000000g")
	assert.Error(err)

	// sha2 not supported
	r, err = Parse("sha2-0000000000000000000000000000000000000000")
	assert.Error(err)

	r, err = Parse("sha1-0000000000000000000000000000000000000000")
	assert.NoError(err)
	assert.NotNil(r)
}

func TestEquals(t *testing.T) {
	assert := assert.New(t)

	r0 := MustParse("sha1-0000000000000000000000000000000000000000")
	r01 := MustParse("sha1-0000000000000000000000000000000000000000")
	r1 := MustParse("sha1-0000000000000000000000000000000000000001")

	assert.Equal(r0, r01)
	assert.Equal(r01, r0)
	assert.NotEqual(r0, r1)
	assert.NotEqual(r1, r0)
}

func TestString(t *testing.T) {
	s := "sha1-0123456789abcdef0123456789abcdef01234567"
	r := MustParse(s)
	assert.Equal(t, s, r.String())
}