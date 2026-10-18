package idgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNextID_Unique(t *testing.T) {
	svc := New(1)
	a, b := svc.NextID(), svc.NextID()
	assert.NotEqual(t, a, b)
	assert.Positive(t, a)
}
