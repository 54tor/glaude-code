package main

import (
	"regexp"
	"strings"

	"github.com/brianvoe/gofakeit/v7"
)

const (
	namesMax    = 32
	namesTries  = 200
	rosterNamRe = "^[a-z]{2,12}$"
)

var firstNameRe = regexp.MustCompile(rosterNamRe)

// drawNames tire n prénoms ASCII en minuscules, distincts entre eux et de taken.
func drawNames(n int, taken []string, seed int64) ([]string, error) {
	out := []string{}
	if n <= 0 {
		return out, nil
	}
	n = min(n, namesMax)
	used := map[string]bool{}
	for _, t := range taken {
		used[strings.ToLower(t)] = true
	}
	f := gofakeit.New(uint64(seed))
	for len(out) < n {
		found := false
		for i := 0; i < namesTries && !found; i++ {
			c := strings.ToLower(f.FirstName())
			if firstNameRe.MatchString(c) && !used[c] {
				used[c] = true
				out = append(out, c)
				found = true
			}
		}
		if !found {
			return nil, terr("srv.teams_names.tiragePossible")
		}
	}
	return out, nil
}
