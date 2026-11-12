package quality

import (
	"reflect"
	"testing"
)

func TestEvaluateUnknownSeedersOptions(t *testing.T) {
	year := 2020
	zero, low, enough, negative := 0, 2, 3, -1
	target := Target{Title: "Test Movie", Year: &year}
	for _, tc := range []struct {
		name     string
		protocol string
		minimum  int
		seeders  *int
		allow    bool
		eligible bool
		reason   string
	}{
		{name: "default rejects unknown at zero", reason: "Seed count is unknown or below the minimum."},
		{name: "announce permits unknown at zero", allow: true, eligible: true},
		{name: "default rejects unknown at positive minimum", minimum: 3, reason: "Seed count is unknown; the profile requires at least 3 seeders."},
		{name: "announce permits unknown at positive minimum", minimum: 3, allow: true, eligible: true},
		{name: "known zero satisfies zero minimum", seeders: &zero, allow: true, eligible: true},
		{name: "known low rejected despite exception", minimum: 3, seeders: &low, allow: true, reason: "Seed count is unknown or below the minimum."},
		{name: "known negative rejected at zero", seeders: &negative, allow: true, reason: "Seed count is unknown or below the minimum."},
		{name: "known sufficient accepted normally", minimum: 3, seeders: &enough, eligible: true},
		{name: "known sufficient accepted with exception", minimum: 3, seeders: &enough, allow: true, eligible: true},
		{name: "usenet remains exempt", protocol: "usenet", minimum: 3, eligible: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Profile{Qualities: []int{1080}, Cutoff: 1080, Language: "any", MinSeeders: tc.minimum, MaxSizeGiB: 10}
			r := Release{Protocol: tc.protocol, Title: "Test.Movie.2020.1080p.WEB", Size: 1 << 30, Seeders: tc.seeders}
			before := r
			d := EvaluateWithOptions(p, target, r, EvaluateOptions{AllowUnknownSeeders: tc.allow})
			if d.Eligible != tc.eligible {
				t.Fatalf("eligible = %v, want %v: %+v", d.Eligible, tc.eligible, d)
			}
			wantReasons := []string{}
			if tc.reason != "" {
				wantReasons = append(wantReasons, tc.reason)
			}
			if !reflect.DeepEqual(d.Reasons, wantReasons) {
				t.Errorf("reasons = %v, want %v", d.Reasons, wantReasons)
			}
			if !reflect.DeepEqual(Evaluate(p, target, r), EvaluateWithOptions(p, target, r, EvaluateOptions{})) {
				t.Error("Evaluate differs from zero options")
			}
			if !reflect.DeepEqual(r, before) {
				t.Error("evaluation changed release evidence")
			}
		})
	}
}

func TestEvaluateUnknownSeedersDoesNotBypassOtherReasons(t *testing.T) {
	year := 2020
	p := Profile{Qualities: []int{1080}, Cutoff: 1080, Language: "english", MaxSizeGiB: 10}
	target := Target{Title: "Test Movie", Year: &year}
	r := Release{Title: "Other.Movie.2020.720p", Size: 0}
	conservative := Evaluate(p, target, r)
	exception := EvaluateWithOptions(p, target, r, EvaluateOptions{AllowUnknownSeeders: true})
	want := []string{}
	for _, reason := range conservative.Reasons {
		if reason != "Seed count is unknown or below the minimum." {
			want = append(want, reason)
		}
	}
	if exception.Eligible || !reflect.DeepEqual(exception.Reasons, want) {
		t.Fatalf("exception changed other rejections: %+v; want reasons %v", exception, want)
	}
}
