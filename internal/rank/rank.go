// Package rank generates fractional-index keys: strings that sort with byte
// order (Postgres COLLATE "C") so an item can move between two neighbours by
// rewriting only its own key.
//
// A key is an integer part followed by an optional fraction, after David
// Greenspan's "Implementing Fractional Indexing". The integer part's first
// character encodes its length, so appending to the end grows keys slowly:
// a0, a1, … az, b00, b01, …
package rank

import (
	"errors"
	"strings"
)

const digits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// smallestInteger has no key before it.
var smallestInteger = "A" + strings.Repeat("0", 26)

var ErrInvalid = errors.New("rank: invalid key or order")

// Between returns a key that sorts strictly after a and before b. An empty a
// means "before b, at the start"; an empty b means "after a, at the end".
func Between(a, b string) (string, error) {
	if a != "" {
		if err := validate(a); err != nil {
			return "", err
		}
	}
	if b != "" {
		if err := validate(b); err != nil {
			return "", err
		}
	}
	if a != "" && b != "" && a >= b {
		return "", ErrInvalid
	}

	switch {
	case a == "" && b == "":
		return "a0", nil
	case a == "":
		ib := integerPart(b)
		fb := b[len(ib):]
		if ib == smallestInteger {
			m, err := midpoint("", fb, true)
			return ib + m, err
		}
		if ib < b {
			return ib, nil
		}
		if d, ok := decrement(ib); ok {
			return d, nil
		}
		return "", ErrInvalid
	case b == "":
		ia := integerPart(a)
		if i, ok := increment(ia); ok {
			return i, nil
		}
		m, err := midpoint(a[len(ia):], "", false)
		return ia + m, err
	}

	ia, ib := integerPart(a), integerPart(b)
	if ia == ib {
		m, err := midpoint(a[len(ia):], b[len(ib):], true)
		return ia + m, err
	}
	i, ok := increment(ia)
	if !ok {
		return "", ErrInvalid
	}
	if i < b {
		return i, nil
	}
	m, err := midpoint(a[len(ia):], "", false)
	return ia + m, err
}

// midpoint returns a fraction between a and b, where hasB=false means b is
// unbounded above. Neither may end in the zero digit.
func midpoint(a, b string, hasB bool) (string, error) {
	if hasB && a >= b {
		return "", ErrInvalid
	}
	if strings.HasSuffix(a, "0") || (hasB && strings.HasSuffix(b, "0")) {
		return "", ErrInvalid
	}
	if hasB {
		n := 0
		for n < len(b) && digitAt(a, n) == b[n] {
			n++
		}
		if n > 0 {
			m, err := midpoint(a[min(n, len(a)):], b[n:], true)
			return b[:n] + m, err
		}
	}
	da := 0
	if a != "" {
		da = strings.IndexByte(digits, a[0])
	}
	db := len(digits)
	if hasB {
		db = strings.IndexByte(digits, b[0])
	}
	if db-da > 1 {
		return string(digits[(da+db+1)/2]), nil
	}
	if hasB && len(b) > 1 {
		return b[:1], nil
	}
	rest := ""
	if a != "" {
		rest = a[1:]
	}
	m, err := midpoint(rest, "", false)
	return string(digits[da]) + m, err
}

// digitAt pads a with zero digits on the right.
func digitAt(a string, i int) byte {
	if i < len(a) {
		return a[i]
	}
	return digits[0]
}

func integerLength(head byte) (int, bool) {
	switch {
	case head >= 'a' && head <= 'z':
		return int(head-'a') + 2, true
	case head >= 'A' && head <= 'Z':
		return int('Z'-head) + 2, true
	}
	return 0, false
}

// integerPart assumes a validated key.
func integerPart(key string) string {
	n, _ := integerLength(key[0])
	return key[:n]
}

func validate(key string) error {
	n, ok := integerLength(key[0])
	if !ok || len(key) < n || key == smallestInteger {
		return ErrInvalid
	}
	for i := 1; i < len(key); i++ {
		if strings.IndexByte(digits, key[i]) < 0 {
			return ErrInvalid
		}
	}
	if len(key) > n && strings.HasSuffix(key, "0") {
		return ErrInvalid
	}
	return nil
}

func increment(x string) (string, bool) {
	head, digs := x[0], []byte(x[1:])
	carry := true
	for i := len(digs) - 1; carry && i >= 0; i-- {
		d := strings.IndexByte(digits, digs[i]) + 1
		if d == len(digits) {
			digs[i] = digits[0]
		} else {
			digs[i] = digits[d]
			carry = false
		}
	}
	if !carry {
		return string(head) + string(digs), true
	}
	switch head {
	case 'Z':
		return "a" + string(digits[0]), true
	case 'z':
		return "", false
	}
	h := head + 1
	if h > 'a' {
		digs = append(digs, digits[0])
	} else {
		digs = digs[:len(digs)-1]
	}
	return string(h) + string(digs), true
}

func decrement(x string) (string, bool) {
	head, digs := x[0], []byte(x[1:])
	borrow := true
	for i := len(digs) - 1; borrow && i >= 0; i-- {
		d := strings.IndexByte(digits, digs[i]) - 1
		if d == -1 {
			digs[i] = digits[len(digits)-1]
		} else {
			digs[i] = digits[d]
			borrow = false
		}
	}
	if !borrow {
		return string(head) + string(digs), true
	}
	switch head {
	case 'a':
		return "Z" + string(digits[len(digits)-1]), true
	case 'A':
		return "", false
	}
	h := head - 1
	if h < 'Z' {
		digs = append(digs, digits[len(digits)-1])
	} else {
		digs = digs[:len(digs)-1]
	}
	return string(h) + string(digs), true
}
