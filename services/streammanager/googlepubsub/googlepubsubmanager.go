package googlepubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/pubsub"
	"github.com/rudderlabs/rudder-server/utils/logger"
	"github.com/tidwall/gjson"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Config struct {
	Credentials     string              `json:"credentials"`
	ProjectId       string              `json:"projectId"`
	EventToTopicMap []map[string]string `json:"eventToTopicMap"`
}

type pubsubClient struct {
	Pbs          *pubsub.Client
	EventToTopic []map[string]interface{}
}

// NewProducer creates a producer based on destination config
func NewProducer(destinationConfig interface{}) (*pubsubClient, error) {
	var config Config
	ctx := context.Background()
	jsonConfig, err := json.Marshal(destinationConfig)
	if err != nil {
		return nil, fmt.Errorf("[GooglePubSub] Error while marshalling destination config :: %w", err)
	}
	err = json.Unmarshal(jsonConfig, &config)
	if err != nil {
		return nil, fmt.Errorf("[GooglePubSub] error  :: error in GooglePubSub while unmarshelling destination config:: %w", err)
	}
	var client *pubsub.Client
	if config.Credentials != "" && config.ProjectId != "" {
		client, err = pubsub.NewClient(ctx, config.ProjectId, option.WithCredentialsJSON([]byte(config.Credentials)))
	}
	if err != nil {
		return nil, err
	}
	var eventToTopic = make([]map[string]interface{}, len(config.EventToTopicMap))
	for i, s := range config.EventToTopicMap {
		topic := client.Topic(s["to"])
		if eventToTopic[i] == nil {
			eventToTopic[i] = make(map[string]interface{})
		}
		eventToTopic[i]["event"] = s["from"]
		eventToTopic[i]["topic"] = topic
	}
	var pbsClient *pubsubClient
	pbsClient = &pubsubClient{client, eventToTopic}
	return pbsClient, nil
}
func Produce(jsonData json.RawMessage, producer interface{}, destConfig interface{}) (statusCode int, respStatus string, responseMessage string) {
	parsedJSON := gjson.ParseBytes(jsonData)
	pbs, ok := producer.(*pubsubClient)
	ctx := context.Background()
	if !ok {
		respStatus = "Failure"
		responseMessage = "[GooglePubSub] error :: Could not create producer"
		return 400, respStatus, responseMessage
	}
	var data interface{}
	if parsedJSON.Get("message").Value() != nil {
		data = parsedJSON.Get("message").Value()
	} else {
		respStatus = "Failure"
		responseMessage = "[GooglePubSub] error :: message from payload not found"
		return 400, respStatus, responseMessage
	}
	value, err := json.Marshal(data)

	if err != nil {
		respStatus = "Failure"
		responseMessage = "[GooglePubSub] error  :: " + err.Error()
		logger.Errorf("[GooglePubSub] error  :: %w", err)
		statusCode := 400
		return statusCode, respStatus, responseMessage
	}
	if parsedJSON.Get("topicId").Value() != nil {
		topicIdString, ok := parsedJSON.Get("topicId").Value().(string)
		if !ok {
			respStatus = "Failure"
			responseMessage = "[GooglePubSub] error :: Could not parse topic id to string"
			logger.Error(responseMessage)
			statusCode := 400
			return statusCode, respStatus, responseMessage
		}
		if topicIdString == "" {
			respStatus = "Failure"
			responseMessage = "[GooglePubSub] error :: empty topic id string"
			return 400, respStatus, responseMessage
		}
		var topic *pubsub.Topic
		for _, s := range pbs.EventToTopic {
			t := s["topic"].(*pubsub.Topic)
			splitString := strings.Split(t.String(), "/")
			if splitString[3] == topicIdString {
				topic = s["topic"].(*pubsub.Topic)
				break
			}
		}
		if topic == nil {
			statusCode = 400
			responseMessage = "[GooglePubSub] error :: Topic not found in project"
			respStatus = "Failure"
			return statusCode, respStatus, responseMessage
		}
		topic.PublishSettings.DelayThreshold = 0
		result := topic.Publish(ctx, &pubsub.Message{Data: []byte(value)})
		serverID, err := result.Get(ctx)
		if err != nil {
			statusCode = getError(err)
			responseMessage = "[GooglePubSub] error :: Failed to publish:" + err.Error()
			respStatus = "Failure"
			return statusCode, respStatus, responseMessage
		} else {
			responseMessage = "Message publish with serverID" + serverID
		}
		respStatus = "Success"
		return 200, respStatus, responseMessage
	} else {
		respStatus = "Failure"
		responseMessage = "[GooglePubSub] error  :: Topic Id not found"
		return 400, respStatus, responseMessage
	}
}

//CloseProducer closes a given producer
func CloseProducer(producer interface{}) error {
	pbs, ok := producer.(*pubsub.Client)
	if ok {
		err := pbs.Close()
		if err != nil {
			logger.Errorf("error in closing Google Pub/Sub producer: %s", err.Error())
		}
		return err
	}
	return fmt.Errorf("error while closing producer")

}
func getError(err error) (statusCode int) {
	switch status.Code(err) {
	case codes.Canceled:
		statusCode = 499
		break
	case codes.Unknown:
	case codes.InvalidArgument:
	case codes.FailedPrecondition:
	case codes.Aborted:
	case codes.OutOfRange:
	case codes.Unimplemented:
	case codes.DataLoss:
		statusCode = 400
		break
	case codes.DeadlineExceeded:
		statusCode = 504
		break
	case codes.NotFound:
		statusCode = 404
		break
	case codes.AlreadyExists:
		statusCode = 409
		break
	case codes.PermissionDenied:
		statusCode = 403
		break
	case codes.ResourceExhausted:
		statusCode = 429
		break
	case codes.Internal:
		statusCode = 500
		break
	case codes.Unavailable:
		statusCode = 503
		break
	case codes.Unauthenticated:
		statusCode = 401
		break
	default:
		statusCode = 400
		break
	}
	return statusCode
}
