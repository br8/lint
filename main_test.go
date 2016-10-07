package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFetchFileInfo(t *testing.T) {
}

func TestParseFilesToIgnore(t *testing.T) {
	*ignoreFile = "lintignore_test"
	parseFilesToIgnore()
	assert.Equal(t, len(ignored), 5, "Ignored length incorrect")
	assert.Equal(t, ignored[0].itype, DIR, "Itype should have been DIR")
	assert.Equal(t, ignored[1].itype, FILE, "Itype should have been FILE")
	assert.Equal(t, ignored[3].itype, EXT, "Itype should have been EXT")
}

func TestValidateFile(t *testing.T) {
	*ignoreFile = "lintignore_test"
	parseFilesToIgnore()
	assert.False(t, lintFile("folder1/random.pb.go"))
	assert.False(t, lintFile("random.pb.go"))
	assert.False(t, lintFile("vendor/random.go"))
	assert.False(t, lintFile("folder/subfolder/random.go"))
	assert.True(t, lintFile("folder/dontignore.go"))
	assert.True(t, lintFile("lint.go"))
	assert.False(t, lintFile("ignore.go"))
}
