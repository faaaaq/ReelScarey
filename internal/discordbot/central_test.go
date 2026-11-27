package discordbot

import (
	"database/sql"
	"testing"
)

func centralizeBotRequestAccounts(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO accounts(username,password_hash,disabled,requests_access,request_account_id,created_at,updated_at)
		SELECT r.username,r.password_hash,r.disabled,1,r.id,r.created_at,r.created_at FROM request_accounts r
		WHERE NOT EXISTS(SELECT 1 FROM accounts a WHERE a.request_account_id=r.id)`)
	if err != nil {
		t.Fatal(err)
	}
}
