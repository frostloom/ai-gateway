package main

import (
	"fmt"
	"github.com/segmentio/kafka-go"
)

func main() {
	b := &kafka.Hash{}
	for _, id := range []string{"req_0", "req_1", "req_2"} {
		fmt.Printf("%s", id)
		for _, phase := range []string{"reserve", "usage", "settle"} {
			key := id + ":" + phase
			fmt.Printf(" %s=%d", phase, b.Balance(kafka.Message{Key: []byte(key)}, 0, 1, 2))
		}
		fmt.Println()
	}
}
