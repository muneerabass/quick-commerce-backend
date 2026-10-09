package main

import (
	"log"
	"quick-commerce/configs"
	"quick-commerce/internal/api"
)

func main() {

	cfg, err := configs.LoadSettings()
	if err != nil {
		log.Fatal(err)
	}

	if err := api.StartServer(&cfg); err != nil {
		log.Fatal(err)
	}
}
