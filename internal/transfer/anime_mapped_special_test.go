package transfer

import "testing"

func TestMappedSpecialLabelUsesParsedFamily(t *testing.T) {
	for label, want := range map[string]bool{
		"S0E1": true, "S00E01": true, "SP1": true, "ova2": true,
		"ONA1": true, "OAD1": true, "S01E01": false, "1": false,
	} {
		if got := mappedSpecialLabel(label); got != want {
			t.Errorf("mappedSpecialLabel(%q) = %v, want %v", label, got, want)
		}
	}
}
