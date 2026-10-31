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

// drawNames tire count prénoms ASCII en minuscules, distincts entre eux et de taken.
func drawNames(count int, taken []string, seed int64) ([]string, error) {
	names := []string{}
	if count <= 0 {
		return names, nil
	}
	count = min(count, namesMax)
	used := map[string]bool{}
	for _, takenName := range taken {
		used[strings.ToLower(takenName)] = true
	}
	faker := gofakeit.New(uint64(seed))
	for len(names) < count {
		found := false
		for attempt := 0; attempt < namesTries && !found; attempt++ {
			candidate := strings.ToLower(faker.FirstName())
			if firstNameRe.MatchString(candidate) && !used[candidate] {
				used[candidate] = true
				names = append(names, candidate)
				found = true
			}
		}
		if !found {
			return nil, terr("srv.teams_names.tiragePossible")
		}
	}
	return names, nil
}
