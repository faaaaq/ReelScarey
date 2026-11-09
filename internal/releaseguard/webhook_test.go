package releaseguard

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseSABnzbd(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      WebhookResult
		forbidden string
		wantErr   bool
	}{
		{
			name: "json complete",
			body: `{"type":"download","event":"complete","name":"Some.Release.1080p-GRP","nzo_id":"SABnzbd_nzo_abc123","category":"reelscarey","status":"Completed","fail_msg":""}`,
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Some.Release.1080p-GRP",
				JobID: "SABnzbd_nzo_abc123", Category: "reelscarey", State: StateCompleted,
			},
		},
		{
			name: "form failed",
			body: `type=download&event=failed&name=Broken.Release&nzo_id=SABnzbd_nzo_fail&category=movies&status=Failed&fail_msg=Unpack+failed`,
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Broken.Release",
				JobID: "SABnzbd_nzo_fail", Category: "movies", State: StateFailed,
				FailureReason: "Unpack failed",
			},
		},
		{
			name: "status without event",
			body: `{"type":"download","name":"Status.Release","nzo_id":"SABnzbd_nzo_status","category":"tv","status":"Completed"}`,
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Status.Release",
				JobID: "SABnzbd_nzo_status", Category: "tv", State: StateCompleted,
			},
		},
		{
			name: "notification type complete",
			body: `{"type":"complete","nzb_name":"Old.Release","nzo_id":"SABnzbd_nzo_old"}`,
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Old.Release",
				JobID: "SABnzbd_nzo_old", State: StateCompleted,
			},
		},
		{
			name: "uppercase event",
			body: `{"event":"COMPLETED","name":"Upper.Release","nzo_id":"SABnzbd_nzo_upper"}`,
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Upper.Release",
				JobID: "SABnzbd_nzo_upper", State: StateCompleted,
			},
		},
		{
			name: "numeric job id",
			body: `{"event":"complete","name":"Numeric.Release","nzo_id":12345,"category":"movies"}`,
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Numeric.Release",
				JobID: "12345", Category: "movies", State: StateCompleted,
			},
		},
		{
			name:      "unknown credentials are not copied",
			body:      `{"event":"complete","name":"Secret.Release","nzo_id":"SABnzbd_nzo_secret","category":"movies","apikey":"super-secret"}`,
			forbidden: "super-secret",
			want: WebhookResult{
				Provider: WebhookProviderSABnzbd, ReleaseName: "Secret.Release",
				JobID: "SABnzbd_nzo_secret", Category: "movies", State: StateCompleted,
			},
		},
		{
			name:    "non-terminal added event",
			body:    `{"type":"download","event":"download","name":"Added.Release","nzo_id":"SABnzbd_nzo_added"}`,
			wantErr: true,
		},
		{
			name:    "missing identity",
			body:    `{"event":"complete","category":"reelscarey"}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			body:    `{"event":"complete"`,
			wantErr: true,
		},
		{
			name:    "array rejected",
			body:    `[{"event":"complete"}]`,
			wantErr: true,
		},
		{
			name:    "oversized",
			body:    strings.Repeat("a", MaxWebhookBody+1),
			wantErr: true,
		},
	}
	runWebhookCases(t, ParseSABnzbd, tests)
}

func TestParseNZBGet(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      WebhookResult
		forbidden string
		wantErr   bool
	}{
		{
			name: "json success",
			body: `{"NZBID":123,"NZBName":"NZBGet.Release.1080p","NZBCategory":"reelscarey","NZBPP_TOTALSTATUS":"SUCCESS","NZBPP_STATUS":"SUCCESS/ALL"}`,
			want: WebhookResult{
				Provider: WebhookProviderNZBGet, ReleaseName: "NZBGet.Release.1080p",
				JobID: "123", Category: "reelscarey", State: StateCompleted,
			},
		},
		{
			name: "form failure with detail",
			body: `NZBID=42&NZBName=Damaged.Release&NZBCategory=tv&NZBPP_TOTALSTATUS=FAILURE&NZBPP_STATUS=FAILURE%2FHEALTH&NZBPP_NZBNAME=Damaged.Release&message=Par+repair+failed`,
			want: WebhookResult{
				Provider: WebhookProviderNZBGet, ReleaseName: "Damaged.Release",
				JobID: "42", Category: "tv", State: StateFailed, FailureReason: "Par repair failed",
			},
		},
		{
			name: "warning is failed",
			body: `{"NZBPP_NZBNAME":"Warning.Release","NZBID":7,"NZBPP_CATEGORY":"movies","NZBPP_TOTALSTATUS":"WARNING","NZBPP_STATUS":"WARNING/SPACE"}`,
			want: WebhookResult{
				Provider: WebhookProviderNZBGet, ReleaseName: "Warning.Release",
				JobID: "7", Category: "movies", State: StateFailed, FailureReason: "WARNING/SPACE",
			},
		},
		{
			name: "status detail without total",
			body: `{"NZBName":"Detail.Release","NZBID":8,"NZBPP_STATUS":"FAILURE/PAR"}`,
			want: WebhookResult{
				Provider: WebhookProviderNZBGet, ReleaseName: "Detail.Release",
				JobID: "8", State: StateFailed, FailureReason: "FAILURE/PAR",
			},
		},
		{
			name: "lowercase keys",
			body: `{"nzbid":99,"nzbname":"Lower.Release","nzbcategory":"movies","nzbpp_totalstatus":"SUCCESS"}`,
			want: WebhookResult{
				Provider: WebhookProviderNZBGet, ReleaseName: "Lower.Release",
				JobID: "99", Category: "movies", State: StateCompleted,
			},
		},
		{
			name:      "unknown credentials are not copied",
			body:      `{"NZBID":11,"NZBName":"Secret.NZB","NZBPP_TOTALSTATUS":"SUCCESS","password":"super-secret"}`,
			forbidden: "super-secret",
			want: WebhookResult{
				Provider: WebhookProviderNZBGet, ReleaseName: "Secret.NZB",
				JobID: "11", State: StateCompleted,
			},
		},
		{
			name:    "missing state",
			body:    `{"NZBID":1,"NZBName":"No.State"}`,
			wantErr: true,
		},
		{
			name:    "missing identity",
			body:    `{"NZBPP_TOTALSTATUS":"SUCCESS"}`,
			wantErr: true,
		},
		{
			name:    "malformed json",
			body:    `{"NZBPP_TOTALSTATUS":"SUCCESS"`,
			wantErr: true,
		},
		{
			name:    "oversized",
			body:    strings.Repeat("a", MaxWebhookBody+1),
			wantErr: true,
		},
	}
	runWebhookCases(t, ParseNZBGet, tests)
}

func TestWebhookIdentityKeyFallsBackToTitle(t *testing.T) {
	guid := "SABnzbd_nzo_key"
	wantID, err := UsenetKey(guid)
	if err != nil {
		t.Fatal(err)
	}
	gotID, err := (WebhookResult{JobID: guid}).IdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if gotID != wantID {
		t.Fatalf("IdentityKey() = %q, want %q", gotID, wantID)
	}

	title := "Some.Release.1080p"
	wantTitle, err := UsenetTitleKey(title)
	if err != nil {
		t.Fatal(err)
	}
	gotTitle, err := (WebhookResult{ReleaseName: title}).IdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if gotTitle != wantTitle {
		t.Fatalf("IdentityKey() = %q, want %q", gotTitle, wantTitle)
	}

	if _, err := (WebhookResult{}).IdentityKey(); err == nil {
		t.Fatal("expected an identity error for an empty result")
	}
}

func runWebhookCases(t *testing.T, parse func([]byte) (WebhookResult, error), tests []struct {
	name      string
	body      string
	want      WebhookResult
	forbidden string
	wantErr   bool
}) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parse([]byte(test.body))
			if test.wantErr {
				if err == nil {
					t.Fatalf("parse succeeded with %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("parse result = %+v, want %+v", got, test.want)
			}
			if test.forbidden != "" && strings.Contains(fmt.Sprintf("%+v", got), test.forbidden) {
				t.Fatalf("result leaked a forbidden field: %+v", got)
			}
		})
	}
}
