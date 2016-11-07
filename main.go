package main

import (
	"flag"
	"log"

	"github.com/RackHD/voyager-cisco-engine/engine"
	"github.com/RackHD/voyager-utilities/random"
	"github.com/streadway/amqp"
)

var (
	uri          = flag.String("uri", "amqp://guest:guest@rabbitmq:5672/", "AMQP URI")
	exchange     = flag.String("exchange", "voyager-cisco-engine", "Durable, non-auto-deleted AMQP exchange name")
	exchangeType = flag.String("exchange-type", "topic", "Exchange type - direct|fanout|topic|x-custom")
	queue        = flag.String("queue", "voyager-cisco-engine-queue", "Ephemeral AMQP queue name")
	bindingKey   = flag.String("key", "requests", "AMQP binding key")
	consumerTag  = flag.String("consumer-tag", "simple-consumer", "AMQP consumer tag (should not be blank)")
	dbAddress    = flag.String("db-address", "root@(mysql:3306)/mysql", "Address of the MySQL database")
)

const (
	inventoryService = "voyager-inventory-service"
)

func init() {
	flag.Parse()
}

func main() {
	engine := engine.NewEngine(*uri, *dbAddress)
	defer engine.MQ.Close()

	ciscoQueueName := random.RandQueue()
	_, err := StartListening(*exchange, *exchangeType, ciscoQueueName, *bindingKey, *consumerTag, engine)
	if err != nil {
		log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
	}

	inventoryQueueName := random.RandQueue()
	_, err = StartListening(inventoryService, *exchangeType, inventoryQueueName, "node.#", *consumerTag, engine)
	if err != nil {
		log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
	}

	select {}
}

// StartListening begins listening for AMQP messages in the background and kicks of a processor for each
func StartListening(exchangeName, exchangeType, queueName, bindingKey, consumerTag string, engine *engine.Engine) (<-chan amqp.Delivery, error) {
	_, messageChannel, err := engine.MQ.Listen(exchangeName, exchangeType, queueName, bindingKey, consumerTag)
	// Listen for messages in the background in infinite loop
	go func() {
		// Listen for messages in the background in infinite loop
		for m := range messageChannel {
			log.Printf(
				"got %dB delivery: [%v] %s",
				len(m.Body),
				m.DeliveryTag,
				m.Body,
			)
			m.Ack(true)

			go engine.ProcessMessage(&m)
		}
	}()

	return messageChannel, err
}
