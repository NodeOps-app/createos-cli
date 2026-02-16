package main

import (
	"log"
	"os"

	"github.com/NodeOps-app/createos-cli/cmd/root"
)

func main() {
	app := root.NewApp()

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
