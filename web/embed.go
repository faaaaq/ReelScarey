package web

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:dist
var content embed.FS

func Assets() (fs.FS, error) {
	assets, err := fs.Sub(content, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return nil, fmt.Errorf("frontend is not built: run npm ci and npm run build in web before building Go")
	}
	return assets, nil
}
