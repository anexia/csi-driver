// Package mockapi contains a mockgen-generated mock of the go-anxcloud API interface.
package mockapi

import (
	// keeps gomock in go.mod for the generated code and the go:generate directive below.
	_ "github.com/golang/mock/gomock"
)

//go:generate go run github.com/golang/mock/mockgen@v1.6.0 -package mockapi -destination xxgenerated_api.go go.anx.io/go-anxcloud/pkg/api/types API
