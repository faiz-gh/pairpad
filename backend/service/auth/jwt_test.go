package auth

import (
	"testing"
)

func TestCreateJWT(t *testing.T) {

	token, err := CreateJWT(1234)
	if err != nil {
		t.Errorf("error creating JWT: %v", err)
	}

	if token == "" {
		t.Errorf("expectred token to be not empty")
	}

}
