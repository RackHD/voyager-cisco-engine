package engine_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/jinzhu/gorm"
	"github.com/RackHD/voyager-cisco-engine/engine"
	"github.com/RackHD/voyager-utilities/models"
	"github.com/RackHD/voyager-utilities/random"
	"github.com/streadway/amqp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Handler", func() {

	Describe("AMQP Message Handling", func() {
		var rabbitMQURL string
		var sql string
		var Engine *engine.Engine
		var err error

		BeforeEach(func() {
			rabbitMQURL = os.Getenv("RABBITMQ_URL")
			sql = "root@(localhost:3306)/mysql"
			Engine = engine.NewEngine(rabbitMQURL, sql)

			Expect(Engine).ToNot(Equal(nil))

		})
		AfterEach(func() {
			Engine.MQ.Close()
		})

		Context("When a message comes in to inventoryService exchange", func() {
			var inventoryServiceExchange, exchangeType, inventoryServiceBindingKey, inventoryServiceConsumerTag string
			var nodeJSON []byte
			var receiveQueue string
			var inventoryServiceMessages <-chan amqp.Delivery
			var inventoryChannel *amqp.Channel

			BeforeEach(func() {
				// set up AMQP config
				inventoryServiceExchange = "voyager-inventory-service"
				exchangeType = "topic"
				inventoryServiceBindingKey = "*"
				inventoryServiceConsumerTag = "consumer-tag1"

				receiveQueue = random.RandQueue()

				nodeJSON, err = json.Marshal(models.NodeEntityJSON{
					ID:     "fake-node-ID",
					Type:   "switch",
					Status: "Discovered",
				})
				Expect(err).ToNot(HaveOccurred())

				inventoryChannel, inventoryServiceMessages, err = Engine.MQ.Listen(inventoryServiceExchange, exchangeType,
					receiveQueue, inventoryServiceBindingKey, inventoryServiceConsumerTag)
				Expect(err).ToNot(HaveOccurred())

			})

			AfterEach(func() {
				inventoryChannel.Close()

			})

			It("INTEGRATION should generate a config file if node is a switch", func() {
				fakeResponse := "Another fake response"
				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////
				//  Mock out functionality of voyager-secret-service
				//
				secretServiceQueueName := random.RandQueue()
				_, secretServiceMessages, err := Engine.MQ.Listen("voyager-secret-service", "topic", secretServiceQueueName, "requests", "test-tag")
				if err != nil {
					log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
				}
				go func() {
					s := <-secretServiceMessages
					credentials := models.Credentials{
						Username: "admin",
						Password: "V0yag3r!",
					}

					credetialMessage, err := json.Marshal(credentials)
					if err != nil {
						log.Fatalf("Error marshalling credentials %s\n", err)
					}
					err = Engine.MQ.Send(s.Exchange, "topic", s.ReplyTo, string(credetialMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}
				}()
				//
				//
				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////

				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////
				// Mock out the subnet entity entry in the DB
				//
				var db *gorm.DB
				for i := 0; i < 10; i++ {
					db, err = gorm.Open("mysql", sql)
					if err != nil {
						log.Printf("Could not connect. Sleeping for 10s %s\n", err)
						time.Sleep(10 * time.Second)
						continue
					}

					err = db.DB().Ping()
					if err != nil {
						log.Printf("Could not Ping. Sleeping for 10s %s\n", err)
						time.Sleep(10 * time.Second)
						continue
					}
					if hasSubnets := db.HasTable(&models.SubnetEntity{}); !hasSubnets {
						if err = db.CreateTable(&models.SubnetEntity{}).Error; err != nil {
							log.Printf("Error creating Subnet table: %s\n", err)
						}
					}
				}
				newSubnet := models.SubnetEntity{
					ID:     "subnetID",
					Name:   models.DefaultKey,
					PoolID: "poolID",
				}

				err = Engine.MySQL.DB.Create(&newSubnet).Error
				Expect(err).To(BeNil())
				//
				//
				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////

				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////
				//  Mock out functionality of voyager-ipam-service
				//

				ipamQueueName := random.RandQueue()
				_, ipamServiceMessages, err := Engine.MQ.Listen(models.IpamExchange, models.IpamExchangeType, ipamQueueName, models.IpamReceiveQueue, "")
				if err != nil {
					log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
				}
				go func() {
					s := <-ipamServiceMessages
					ipLease := models.IpamLeaseResp{
						IP:          "192.168.1.2",
						Reservation: "reservationID",
					}
					ipLeaseMessage, err := json.Marshal(ipLease)
					if err != nil {
						log.Fatalf("Error marshalling lease %s\n", err)
					}
					err = Engine.MQ.Send(s.Exchange, models.IpamExchangeType, s.ReplyTo, string(ipLeaseMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}
					s = <-ipamServiceMessages
					ipLease = models.IpamLeaseResp{
						IP:          "192.168.1.3",
						Reservation: "reservationID1",
					}
					ipLeaseMessage, err = json.Marshal(ipLease)
					if err != nil {
						log.Fatalf("Error marshalling lease %s\n", err)
					}
					err = Engine.MQ.Send(s.Exchange, models.IpamExchangeType, s.ReplyTo, string(ipLeaseMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}

				}()
				//
				//
				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////

				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////
				//  Mock out functionality of voyager-rackhd-service
				//
				rackhdQueueName := random.RandQueue()
				_, rackhdServiceMessages, err := Engine.MQ.Listen(models.RackHDExchange, models.RackHDExchangeType, rackhdQueueName, models.RackHDBindingKey, "")
				if err != nil {
					log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
				}
				go func() {
					defer GinkgoRecover()
					var s amqp.Delivery

					select {
					case s = <-rackhdServiceMessages:
						break
					case <-time.After(time.Duration(10) * time.Second):
						panic("Timeout")
					}

					var requestConfig models.RackHDConfigReq

					err := json.Unmarshal(s.Body, &requestConfig)
					Expect(err).ToNot(HaveOccurred())

					fileBytes, err := ioutil.ReadFile("../spec_assets/cisco-config")
					Expect(err).ToNot(HaveOccurred())
					expectedOutput := string(fileBytes)

					Expect(requestConfig.Config).To(Equal(expectedOutput))

					rackHDresponse := models.RackHDResp{
						ServerResponse: fakeResponse,
					}
					rackHdMessage, err := json.Marshal(rackHDresponse)
					if err != nil {
						log.Fatalf("Error marshalling response %s\n", err)
					}

					err = Engine.MQ.Send(s.Exchange, models.RackHDExchangeType, s.ReplyTo, string(rackHdMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}

					select {
					case s = <-rackhdServiceMessages:
						break
					case <-time.After(time.Duration(10) * time.Second):
						panic("Timeout")
					}
					var requestDeployTemplate models.RackHDConfigReq
					err = json.Unmarshal(s.Body, &requestDeployTemplate)
					Expect(err).ToNot(HaveOccurred())

					fileBytes, err = ioutil.ReadFile(filepath.Join("../templates", models.RackHDCiscoNexusDeployConfigTemplate))
					Expect(err).ToNot(HaveOccurred())
					expectedOutput = string(fileBytes)

					Expect(requestDeployTemplate.Config).To(Equal(expectedOutput))

					rackHDresponse = models.RackHDResp{
						ServerResponse: fakeResponse,
					}
					rackHdMessage, err = json.Marshal(rackHDresponse)
					if err != nil {
						log.Fatalf("Error marshalling response %s\n", err)
					}

					err = Engine.MQ.Send(s.Exchange, models.RackHDExchangeType, s.ReplyTo, string(rackHdMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}

					select {
					case s = <-rackhdServiceMessages:
						break
					case <-time.After(time.Duration(10) * time.Second):
						panic("Timeout")
					}
					var requestDeployWorkflow models.RackHDWorkflowReq
					err = json.Unmarshal(s.Body, &requestDeployWorkflow)
					Expect(err).ToNot(HaveOccurred())
					fileBytes, err = ioutil.ReadFile(filepath.Join("../workflows", models.RackHDDeployConfigWorkflow))
					Expect(err).ToNot(HaveOccurred())
					expectedOutput = string(fileBytes)
					Expect(requestDeployWorkflow.Workflow).To(Equal(expectedOutput))
					rackHDresponse = models.RackHDResp{
						ServerResponse: fakeResponse,
					}
					rackHdMessage, err = json.Marshal(rackHDresponse)
					if err != nil {
						log.Fatalf("Error marshalling response %s\n", err)
					}

					err = Engine.MQ.Send(s.Exchange, models.RackHDExchangeType, s.ReplyTo, string(rackHdMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}

					expectedWorkFlowConfig := models.RackHDWorkflowConfig{
						Name: models.DefaultCiscoInjectableName,
						Options: models.WorkflowConfigOptions{
							DeployConfigAndImages: models.WorkflowConfigAndImages{
								StartupConfig: requestConfig.Name,
								BootImage:     models.DefaultCiscoBootImage,
							},
						},
					}

					select {
					case s = <-rackhdServiceMessages:
						break
					case <-time.After(time.Duration(10) * time.Second):
						panic("Timeout")
					}
					var requestRunWorkflow models.RackHDRunWorkflowReq
					err = json.Unmarshal(s.Body, &requestRunWorkflow)
					Expect(err).ToNot(HaveOccurred())
					Expect(requestRunWorkflow.RackHDWorkflowConfig).To(Equal(expectedWorkFlowConfig))
					fakeWorkFlowinstanceID := "i-got-id"
					fakeResponse = fmt.Sprintf(`{"instanceId": "%s"}`, fakeWorkFlowinstanceID)
					rackHDresponse = models.RackHDResp{
						ServerResponse: fakeResponse,
					}
					rackHdMessage, err = json.Marshal(rackHDresponse)
					if err != nil {
						log.Fatalf("Error marshalling reponse %s\n", err)
					}

					err = Engine.MQ.Send(s.Exchange, models.RackHDExchangeType, s.ReplyTo, string(rackHdMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}

					expectedRountingKey := fmt.Sprintf("%s.%s", models.GraphFinishedRoutingKey, fakeWorkFlowinstanceID)
					select {
					case s = <-rackhdServiceMessages:
						break
					case <-time.After(time.Duration(10) * time.Second):
						panic("Timeout")
					}
					var requestListenReq models.RackHDListenReq
					err = json.Unmarshal(s.Body, &requestListenReq)
					Expect(err).ToNot(HaveOccurred())
					Expect(requestListenReq.Exchange).To(Equal(models.OnEventsExchange))
					Expect(requestListenReq.ExchangeType).To(Equal(models.OnEventsExchangeType))
					Expect(requestListenReq.RoutingKey).To(Equal(expectedRountingKey))
					rackHDresponse = models.RackHDResp{
						ServerResponse: fakeResponse,
					}
					rackHdMessage, err = json.Marshal(rackHDresponse)
					if err != nil {
						log.Fatalf("Error marshalling reponse %s\n", err)
					}

					err = Engine.MQ.Send(s.Exchange, models.RackHDExchangeType, s.ReplyTo, string(rackHdMessage), s.CorrelationId, s.RoutingKey)
					if err != nil {
						log.Fatalf("Error sending to RabbitMQ: %s\n", err)
					}
				}()
				//
				//
				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////

				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////
				//  Mock out functionality of voyager-inventory-service
				//

				inventoryQueueName := random.RandQueue()
				_, inventoryServiceRequests, err := Engine.MQ.Listen(models.InventoryServiceExchange, models.InventoryServiceExchangeType, inventoryQueueName, models.InventoryServiceBindingKey, "")
				if err != nil {
					log.Fatalf("Error Listening to RabbitMQ: %s\n", err)
				}
				go func() {
					s := <-inventoryServiceRequests
					var cmd models.CmdMessage
					err = json.Unmarshal(s.Body, &cmd)
					Expect(err).ToNot(HaveOccurred())

					nJSON, err := json.Marshal(cmd.Args)
					Expect(err).ToNot(HaveOccurred())

					var node models.NodeEntity
					err = json.Unmarshal(nJSON, &node)

					Expect(err).ToNot(HaveOccurred())
					Expect(node.ID).To(Equal("fake-node-ID"))
					Expect(node.Status).To(Equal(models.StatusAvailableLearning))
					Expect(node.Type).To(Equal(models.NodeTypeSwitch))

					err = Engine.MQ.Send(s.Exchange, models.InventoryServiceExchangeType, s.ReplyTo, "SUCCESS", s.CorrelationId, s.RoutingKey)
					Expect(err).ToNot(HaveOccurred())

				}()
				//
				//
				//////////////////////////////////////////////////////////////////////////////
				//////////////////////////////////////////////////////////////////////////////

				// Send nodeJSON on voyager-inventory-service exchange
				err = Engine.MQ.Send(inventoryServiceExchange, exchangeType, inventoryServiceBindingKey, string(nodeJSON), "", "")
				Expect(err).ToNot(HaveOccurred())

				// Grab message from inventoryServiceMessages channel, call ProcessMessage
				select {
				case m := <-inventoryServiceMessages:
					m.Ack(false)
					err = Engine.ProcessMessage(&m)
					Expect(err).ToNot(HaveOccurred())
				case <-time.After(10 * time.Second):
					Fail("Timeout")
				}

			})

		})

	})

})
