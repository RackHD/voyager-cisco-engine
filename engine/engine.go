package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/RackHD/voyager-cisco-engine/mysql"
	"github.com/RackHD/voyager-utilities/amqp"
	"github.com/RackHD/voyager-utilities/models"
	"github.com/RackHD/voyager-utilities/random"
	log "github.com/sirupsen/logrus"
	samqp "github.com/streadway/amqp"
)

// Engine is engine
type Engine struct {
	MQ    *amqp.Client
	MySQL *mysql.DBconn
}

//TemplateParams is a struct for the configuration template
type TemplateParams struct {
	Username        string
	Password        string
	DiscoveryVlanIP string
	InterfaceRegex  string
	MgmtIP          string
}

// NewEngine creates new engine instance
func NewEngine(amqpAddress string, dbAddress string) *Engine {
	engine := Engine{}
	engine.MQ = amqp.NewClient(amqpAddress)
	if engine.MQ == nil {
		log.Fatalf("Could not connect to RabbitMQ at %s\n", amqpAddress)
	}

	engine.MySQL = &mysql.DBconn{}
	err := engine.MySQL.Connect(dbAddress)
	if err != nil {
		log.Fatalf("Error connecting to DB: %s\n", err)
	}

	return &engine
}

// ProcessMessage processes a message
func (e *Engine) ProcessMessage(m *samqp.Delivery) error {
	switch m.Exchange {

	case "voyager-cisco-engine":
		return e.processCiscoEngine(m)

	case "voyager-inventory-service":
		return e.processInventoryService(m)

	default:
		err := fmt.Errorf("Unknown exchange name: %s\n", m.Exchange)
		log.Warnf("Error: %s", err)
		return err

	}
}

// processInventoryService processes a message from the processInventoryService exchange
func (e *Engine) processInventoryService(d *samqp.Delivery) error {
	log.Printf("Got message %s", d.Body)
	// Unmarshal the message (assumes message is valid json)
	var n models.NodeEntityJSON
	err := json.Unmarshal(d.Body, &n)
	if err != nil {
		log.Warnf("Error: %s\n", err)
		return err
	}

	if n.Type != "switch" {
		log.Infof("Discovered node is not switch. Cisco Engine will ignore")
		return nil
	}

	creds, err := e.getCredentials()
	if err != nil {
		log.Warnf("Error getting credentials: %s\n", err)
		return err
	}

	discoveryVlanIPLease, err := e.getIPLease()
	if err != nil {
		log.Warnf("Error requesting ip lease: %s\n", err)
		return err
	}

	mgmtIPLease, err := e.getIPLease()
	if err != nil {
		log.Warnf("Error requesting ip lease: %s\n", err)
		return err
	}

	templateData := TemplateParams{
		Username:        creds.Username,
		Password:        creds.Password,
		DiscoveryVlanIP: discoveryVlanIPLease.IP,
		InterfaceRegex:  "*",
		MgmtIP:          mgmtIPLease.IP,
	}
	var templateText string
	templateText, err = e.renderCiscoTemplate("cisco-config", templateData)
	if err != nil {
		log.Warnf("Error rendering cisco-config template %s\n", err)
		return err
	}

	log.Printf("****************Temp: Rendered template %s", templateText)
	configTemplateName := fmt.Sprintf("cisco-config-%s", random.RandQueue())
	var rackHDResp models.RackHDResp
	request := models.RackHDConfigReq{
		RackHDReq: models.RackHDReq{
			Action: models.UploadTemplateAction,
		},
		Name:   configTemplateName,
		Config: templateText,
	}

	log.Printf("****************Temp: Sending request to RackHD to upload config %v", request)
	rackHDResp, err = e.sendRequestToRackHD(request, true)
	if err != nil {
		log.Printf("*****TEMP:ERROR: %s", err)
		return err
	}
	if rackHDResp.Failed {
		log.Printf("*******TEMP:ERRROR %v", rackHDResp)
		return fmt.Errorf("%s", rackHDResp.Error)
	}

	log.Printf("****************Temp: Get response from RACKHD %v", rackHDResp)
	fileName := models.RackHDCiscoNexusDeployConfigTemplate
	fullPathToFilename, err := getTemplatePath(fileName)
	if err != nil {
		return err
	}

	templateTextBytes, err := ioutil.ReadFile(fullPathToFilename)
	if err != nil {
		return err
	}
	templateText = string(templateTextBytes)

	request = models.RackHDConfigReq{
		RackHDReq: models.RackHDReq{
			Action: models.UploadTemplateAction,
		},
		Name:   fileName,
		Config: templateText,
	}
	rackHDResp, err = e.sendRequestToRackHD(request, true)
	if err != nil {
		return err
	}

	if rackHDResp.Failed {
		return fmt.Errorf("%s", rackHDResp.Error)
	}

	log.Printf("****************Temp: Deploying config workflow")
	fileName = models.RackHDDeployConfigWorkflow
	fullPathToFilename, err = getWorkflowPath(fileName)
	if err != nil {
		return err
	}
	templateTextBytes, err = ioutil.ReadFile(fullPathToFilename)
	if err != nil {
		return err
	}
	templateText = string(templateTextBytes)
	workflowRequest := models.RackHDWorkflowReq{
		RackHDReq: models.RackHDReq{
			Action: models.UploadWorkflowAction,
		},
		Workflow: templateText,
	}
	rackHDResp, err = e.sendRequestToRackHD(workflowRequest, true)
	if err != nil {
		return err
	}
	if rackHDResp.Failed {
		return fmt.Errorf("%s", rackHDResp.Error)
	}

	runWorkflowRequest := models.RackHDRunWorkflowReq{
		RackHDReq: models.RackHDReq{
			Action: models.RunWorkflowAction,
		},
		NodeID: n.ID,
		RackHDWorkflowConfig: models.RackHDWorkflowConfig{
			Name: models.DefaultCiscoInjectableName,
			Options: models.WorkflowConfigOptions{
				DeployConfigAndImages: models.WorkflowConfigAndImages{
					StartupConfig: configTemplateName,
					BootImage:     models.DefaultCiscoBootImage,
				},
			},
		},
	}

	rackHDResp, err = e.sendRequestToRackHD(runWorkflowRequest, true)
	if err != nil {
		return err
	}
	if rackHDResp.Failed {
		return fmt.Errorf("%s", rackHDResp.Error)
	}
	graphUUID := models.WorkflowInstanceID{}
	err = json.Unmarshal([]byte(rackHDResp.ServerResponse), &graphUUID)
	if err != nil {
		return err
	}

	/////////////////////////////////////////////////////////////////////////
	log.Printf("****************Temp: Tell RackHD to listen to On-Events for workflow complete\n")
	routingKey := fmt.Sprintf("%s.%s", models.GraphFinishedRoutingKey, graphUUID.InstanceID)
	requestMessage := models.RackHDListenReq{}
	requestMessage.Action = models.ListenWorkflowAction
	requestMessage.RoutingKey = routingKey
	requestMessage.Exchange = models.OnEventsExchange
	requestMessage.ExchangeType = models.OnEventsExchangeType

	rackHDResp, err = e.sendRequestToRackHD(requestMessage, false)
	if err != nil {
		return err
	}
	if rackHDResp.Failed {
		return fmt.Errorf("%s", rackHDResp.Error)
	}

	// TODO
	// Cisco-Engine needs to send a message to Inventory-Service to update the
	// Management IP address of the switch. Must get this address from IPAM-Service

	/////////////////////////////////////////////////////////////////////////
	log.Printf("****************Temp: Tell voyager-inventory-service to update node status to \"Available-Learning\"\n")
	NodeEntity := models.NodeEntity{
		ID:     n.ID,
		Type:   models.NodeTypeSwitch,
		Status: models.StatusAvailableLearning,
	}
	Cmd := models.CmdMessage{
		Command: "update_node",
		Args:    &NodeEntity,
	}
	CmdMessage, err := json.Marshal(Cmd)
	if err != nil {
		return err
	}

	// Send request message
	err = e.MQ.Send(models.InventoryServiceExchange, models.InventoryServiceExchangeType, models.InventoryServiceBindingKey, string(CmdMessage), "", "")
	if err != nil {
		return err
	}
	return nil
}

func (e *Engine) sendRequestToRackHD(request interface{}, timeout bool) (models.RackHDResp, error) {
	replyTo := random.RandQueue()
	correlationID := random.RandQueue()
	mqChannel, deliveries, err := e.MQ.Listen(models.RackHDExchange, models.RackHDExchangeType, replyTo, replyTo, models.RackHDConsumerTag)
	if err != nil {
		return models.RackHDResp{}, err
	}
	defer func() {
		log.Infof("Trying to close the channel\n")
		log.Infof("Trying to delete the queue: %s\n", replyTo)
		if _, err = mqChannel.QueueDelete(replyTo, false, false, true); err != nil {
			log.Warnf("Queue Delete failed: %s\n", err)
		}
		mqChannel.Close()
	}()
	var response models.RackHDResp
	message, err := json.Marshal(request)
	if err != nil {
		return models.RackHDResp{}, err
	}
	err = e.MQ.Send(models.RackHDExchange, models.RackHDExchangeType, models.RackHDBindingKey, string(message), correlationID, replyTo)
	if err != nil {
		return models.RackHDResp{}, err
	}
	for {
		select {
		case d := <-deliveries:
			log.Infof("got replies correctionID 1: %s, correlationID2: %s", d.CorrelationId, correlationID)
			if d.CorrelationId == correlationID {

				log.Printf("*********TEMP: the response body was %s", string(d.Body))
				err := json.Unmarshal(d.Body, &response)
				if err != nil {
					log.Printf("Error unmarshaling: %s\n", err)
					return response, err
				}
				return response, nil
			}
		case <-time.After(5 * time.Second):
			if timeout {

				return response, fmt.Errorf("Error: RackHD did not respond: timeout")
			}
		}
	}
}

// getCredentials requests credentials from voyager-secret-service via RMQ and listens for reply
func (e *Engine) getCredentials() (models.Credentials, error) {
	queueName := random.RandQueue()
	bindingKey := "replies"
	replyTo := bindingKey
	correlationID := random.RandQueue()
	consumerTag := random.RandQueue()

	mqChannel, deliveries, err := e.MQ.Listen(models.SecretExchange, models.SecretExchangeType, queueName, bindingKey, consumerTag)
	if err != nil {
		log.Warnf("Could not listen to voyager-secret-service exchange: %s\n", err)
		return models.Credentials{}, err
	}

	defer func() {
		log.Infof("Trying to close the channel\n")
		if err = mqChannel.Cancel(consumerTag, false); err != nil {
			log.Warnf("Consumer cancel failed: %s\n", err)
		}

		log.Infof("Trying to delete the queue: %s\n", queueName)
		if _, err = mqChannel.QueueDelete(queueName, false, false, true); err != nil {
			log.Warnf("Queue Delete failed: %s\n", err)
		}

	}()

	err = e.MQ.Send(models.SecretExchange, models.SecretExchangeType, models.SecretBindingKey, "generatePassword", correlationID, replyTo)
	if err != nil {
		log.Warnf("Error sending credentials request: %s\n", err)
		return models.Credentials{}, err

	}
	for {
		select {
		case d := <-deliveries:
			if d.CorrelationId == correlationID {
				var creds models.Credentials

				err := json.Unmarshal(d.Body, &creds)
				if err != nil {
					log.Warnf("Error unmarshaling: %s\n", err)
					return models.Credentials{}, err
				}

				return creds, nil
			}
		case <-time.After(5 * time.Second):
			return models.Credentials{}, fmt.Errorf("ERROR: Timeout getting credentials")
		}
	}
}

func (e *Engine) getIPLease() (models.IpamLeaseResp, error) {
	bindingKey := "replies"
	replyTo := bindingKey
	correlationID := random.RandQueue()
	consumerTag := random.RandQueue()
	queueName := random.RandQueue()

	mqChannel, deliveries, err := e.MQ.Listen(models.IpamExchange, models.IpamExchangeType, queueName, bindingKey, consumerTag)
	if err != nil {
		log.Warnf("Could not listen to %s exchange: %s\n", models.IpamExchange, err)
		return models.IpamLeaseResp{}, err
	}

	defer func() {
		log.Infof("Trying to close the channel\n")
		if err = mqChannel.Cancel(consumerTag, false); err != nil {
			log.Warnf("Consumer cancel failed: %s\n", err)
		}
	}()

	subnetID, err := e.getSubnetIDByName(models.DefaultKey)
	if err != nil {
		log.Println(err)
		return models.IpamLeaseResp{}, err
	}

	ipReq := models.IpamLeaseReq{
		Action:   models.RequestIPAction,
		SubnetID: subnetID,
	}

	ipReqBytes, err := json.Marshal(ipReq)
	if err != nil {
		return models.IpamLeaseResp{}, err
	}

	err = e.MQ.Send(models.IpamExchange, models.IpamExchangeType, models.IpamReceiveQueue, string(ipReqBytes), correlationID, replyTo)
	if err != nil {
		log.Warnf("Error sending ip request: %s\n", err)
		return models.IpamLeaseResp{}, err
	}
	for {
		select {
		case d := <-deliveries:
			if d.CorrelationId == correlationID {
				var ipLease models.IpamLeaseResp
				err = json.Unmarshal(d.Body, &ipLease)
				if err != nil {
					log.Warnf("Error unmarshaling: %s\n", err)
					return models.IpamLeaseResp{}, err
				}
				return ipLease, nil
			}
		case <-time.After(5 * time.Second):
			return models.IpamLeaseResp{}, fmt.Errorf("Error receiving Amqp response: %+v", ipReq)
		}
	}
}

func (e *Engine) getSubnetIDByName(subnetName string) (string, error) {
	subnet := models.SubnetEntity{}
	log.Printf("*******************TEMP: Getting subnet ID using subnetName: %s", subnetName)
	// Get new subnet, verify ID was assigned, name is correct, and is associated with pool
	err := e.MySQL.DB.Where("name = ?", subnetName).Find(&subnet).Error
	if err != nil {
		log.Printf("Failed to get subnet ID using subnetName: %s", subnetName)
		return "", err
	}
	log.Printf("*******************TEMP: Got Subnet ID %s", subnet.ID)

	return subnet.ID, nil
}

func getTemplatePath(filename string) (string, error) {
	return filepath.Abs(filepath.Join(".", "../templates", filename))
}

func getWorkflowPath(filename string) (string, error) {
	return filepath.Abs(filepath.Join(".", "../workflows", filename))
}

func (e *Engine) renderCiscoTemplate(filename string, templateData TemplateParams) (string, error) {
	fullPathToFilename, err := getTemplatePath(filename)
	if err != nil {
		return "", err
	}
	ciscoTemplate, err := template.ParseFiles(fullPathToFilename)
	if err != nil {
		return "", err
	}
	var ciscoConfig bytes.Buffer
	err = ciscoTemplate.Execute(&ciscoConfig, templateData)
	if err != nil {
		return "", err
	}
	return string(ciscoConfig.Bytes()), nil
}

// processCiscoEngine processes a message from the CiscoEngine exchange
func (e *Engine) processCiscoEngine(d *samqp.Delivery) error {
	err := fmt.Errorf("Handler not implemented: %s\n", d.Exchange)
	log.Warnf("Error: %s", err)
	return err
}
