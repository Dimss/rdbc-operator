package controller

import (
	"github.com/rdbc-operator/pkg/controller/rdbc"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, rdbc.Add)
}
