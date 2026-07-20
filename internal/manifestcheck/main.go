// Command manifestcheck constructs the middleware from manifest testData JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	stategeo "github.com/vikewoods/traefik-plugin-state-geo/v2"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read manifest test data from stdin: %v\n", err)
		os.Exit(1)
	}

	config := stategeo.CreateConfig()
	if err := json.Unmarshal(data, config); err != nil {
		fmt.Fprintf(os.Stderr, "decode manifest test data: %v\n", err)
		os.Exit(1)
	}

	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusNoContent)
	})
	if _, err := stategeo.New(context.Background(), next, config, "catalog-test-data"); err != nil {
		fmt.Fprintf(os.Stderr, "construct middleware from manifest testData: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Traefik manifest testData constructs the middleware successfully.")
}
