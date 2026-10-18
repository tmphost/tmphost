// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package idgen generates time-ordered, unique int64 IDs.
package idgen

//go:generate mockgen -source=idgen.go -destination=mocks/idgen.go -package=mocks

import (
	"github.com/mustafaturan/monoflake"
)

type (
	// Service issues new IDs.
	Service interface {
		NextID() int64
	}

	service struct {
		mf *monoflake.MonoFlake
	}
)

// New returns an ID generator for the given node. Node IDs wrap at 1024.
func New(node uint16) Service {
	// monoflake.New only fails on invalid options, and none are passed.
	mf, _ := monoflake.New(node)
	return &service{mf: mf}
}

func (s *service) NextID() int64 {
	return s.mf.Next().Int64()
}
