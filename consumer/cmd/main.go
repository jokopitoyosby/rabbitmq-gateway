package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/jokopitoyosby/rabbitmq-gateway/consumer"
	"github.com/jokopitoyosby/rabbitmq-gateway/models"
)

func main() {
	callbacks := map[string]func([]byte) error{

		"positions": func(body []byte) error {
			//fmt.Printf("body: %v\n", string(body))
			var position models.PositionData
			if err := json.Unmarshal(body, &position); err != nil {
				return fmt.Errorf("failed to unmarshal position: %v", err)
			}

			jsonData, err := json.MarshalIndent(position, "", "    ")
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(jsonData))
			return nil
		},
	}
	consumer.NewConsumer("config.json", "position_exchange", []string{"positions"}, callbacks).Start()
	select {}
}
