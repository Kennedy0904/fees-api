package main

import (
	"log"

	"fees-api/internal/fees"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	w := worker.New(c, fees.BillTaskQueue, worker.Options{})
	w.RegisterWorkflow(fees.BillWorkflow)

	log.Printf("fees Temporal worker listening on task queue %q", fees.BillTaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatal(err)
	}
}
