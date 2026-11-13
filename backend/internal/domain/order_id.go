package domain

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
)

var (
	ErrInvalidOrderID = errors.New("invalid order ID format or checksum")
	orderIDRegex      = regexp.MustCompile(`^ORD-(\d{13})-(\d{3})$`)
)

func GenerateOrderID() (string, error) {
	max := big.NewInt(10_000_000_000_000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}

	num := n.Uint64()
	checksum := num % 997

	return fmt.Sprintf("ORD-%013d-%03d", num, checksum), nil
}

func ValidateOrderID(id string) error {
	matches := orderIDRegex.FindStringSubmatch(id)
	if len(matches) != 3 {
		return ErrInvalidOrderID
	}

	num, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return ErrInvalidOrderID
	}

	expectedChecksum, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return ErrInvalidOrderID
	}

	if (num % 997) != expectedChecksum {
		return ErrInvalidOrderID
	}

	return nil
}
