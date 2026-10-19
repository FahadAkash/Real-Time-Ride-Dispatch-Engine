package dispatch

import (
	"context"
	"sort"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
)

type Matcher struct {
	geo    DriverIndex
	locker Locker
}

func NewMatcher(geo DriverIndex, locker Locker) *Matcher {
	return &Matcher{
		geo:    geo,
		locker: locker,
	}
}

// FindAndReserveBestDriver searches nearest candidates, filters available drivers, ranks them, and reserves the first available.
func (m *Matcher) FindAndReserveBestDriver(ctx context.Context, region string, pickup domain.Location, rideID string, excludedDrivers []string, maxCandidates int, lockTTL time.Duration) (*domain.DriverCandidate, error) {
	excludedMap := make(map[string]bool, len(excludedDrivers))
	for _, id := range excludedDrivers {
		excludedMap[id] = true
	}

	// 1. GEOSEARCH nearest candidates within 5km radius
	radiusMeters := 5000.0
	candidates, err := m.geo.SearchNearestDrivers(ctx, region, pickup, radiusMeters, maxCandidates*2)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, domain.ErrNoDriverFound
	}

	// 2. Filter available drivers and excluded drivers
	filtered := make([]domain.DriverCandidate, 0, len(candidates))
	for _, cand := range candidates {
		if excludedMap[cand.DriverID] {
			continue
		}

		state, _, _, err := m.geo.GetDriverStatus(ctx, cand.DriverID)
		if err != nil || state != domain.DriverStateAvailable {
			continue
		}

		// Calculate composite score (shorter distance + higher rating)
		distKm := cand.DistanceMeters / 1000.0
		normDist := 1.0 / (1.0 + distKm)
		score := normDist*0.7 + (cand.Rating/5.0)*0.3
		cand.Score = score

		filtered = append(filtered, cand)
	}

	if len(filtered) == 0 {
		return nil, domain.ErrNoDriverFound
	}

	// 3. Rank: sort descending by score
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Score > filtered[j].Score
	})

	// 4. Atomic reservation: try candidate 1..N
	for _, candidate := range filtered {
		acquired, err := m.locker.AcquireDriverLock(ctx, candidate.DriverID, rideID, lockTTL)
		if err != nil {
			continue
		}
		if acquired {
			return &candidate, nil
		}
	}

	return nil, domain.ErrNoDriverFound
}

