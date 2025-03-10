package predict

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestNewInputsChatMessage(t *testing.T) {
	chatMessage := `[{"role": "user", "content": "hello"}]`
	key := "Key"
	expected := json.RawMessage(chatMessage)
	keyVals := map[string][]string{
		key: {chatMessage},
	}
	openapiBody := `{"components":{"schemas":{"CommonChatSchemaAssistantMessage":{"properties":{"audio":{"$ref":"#/components/schemas/CommonChatSchemaAudio"},"content":{"anyOf":[{"type":"string"},{"items":{"anyOf":[{"$ref":"#/components/schemas/CommonChatSchemaTextContentPart"},{"$ref":"#/components/schemas/CommonChatSchemaRefuslaContentPart"}]},"type":"array"}],"title":"Content"},"function_call":{"$ref":"#/components/schemas/CommonChatSchemaFunction"},"name":{"title":"Name","type":"string"},"role":{"title":"Role","type":"string"},"tool_calls":{"items":{"$ref":"#/components/schemas/CommonChatSchemaToolCall"},"title":"Tool Calls","type":"array"}},"title":"CommonChatSchemaAssistantMessage","type":"object"},"CommonChatSchemaAudio":{"properties":{"id":{"title":"Id","type":"string"}},"required":["id"],"title":"CommonChatSchemaAudio","type":"object"},"CommonChatSchemaAudioContentPart":{"properties":{"input_audio":{"$ref":"#/components/schemas/CommonChatSchemaInputAudio"},"type":{"title":"Type","type":"string"}},"required":["type","input_audio"],"title":"CommonChatSchemaAudioContentPart","type":"object"},"CommonChatSchemaDeveloperMessage":{"properties":{"content":{"anyOf":[{"type":"string"},{"items":{"type":"string"},"type":"array"}],"title":"Content"},"name":{"title":"Name","type":"string"},"role":{"title":"Role","type":"string"}},"title":"CommonChatSchemaDeveloperMessage","type":"object"},"CommonChatSchemaFunction":{"properties":{"arguments":{"title":"Arguments","type":"string"},"name":{"title":"Name","type":"string"}},"required":["name","arguments"],"title":"CommonChatSchemaFunction","type":"object"},"CommonChatSchemaFunctionMessage":{"properties":{"content":{"title":"Content","type":"string"},"name":{"title":"Name","type":"string"},"role":{"title":"Role","type":"string"}},"title":"CommonChatSchemaFunctionMessage","type":"object"},"CommonChatSchemaImageContentPart":{"properties":{"image_url":{"$ref":"#/components/schemas/CommonChatSchemaImageURL"},"type":{"title":"Type","type":"string"}},"required":["type","image_url"],"title":"CommonChatSchemaImageContentPart","type":"object"},"CommonChatSchemaImageURL":{"properties":{"detail":{"title":"Detail","type":"string"},"url":{"title":"Url","type":"string"}},"required":["url","detail"],"title":"CommonChatSchemaImageURL","type":"object"},"CommonChatSchemaInputAudio":{"properties":{"data":{"title":"Data","type":"string"},"format":{"title":"Format","type":"string"}},"required":["data","format"],"title":"CommonChatSchemaInputAudio","type":"object"},"CommonChatSchemaRefuslaContentPart":{"properties":{"refusal":{"title":"Refusal","type":"string"},"type":{"title":"Type","type":"string"}},"required":["type","refusal"],"title":"CommonChatSchemaRefuslaContentPart","type":"object"},"CommonChatSchemaSystemMessage":{"properties":{"content":{"anyOf":[{"type":"string"},{"items":{"type":"string"},"type":"array"}],"title":"Content"},"name":{"title":"Name","type":"string"},"role":{"title":"Role","type":"string"}},"title":"CommonChatSchemaSystemMessage","type":"object"},"CommonChatSchemaTextContentPart":{"properties":{"text":{"title":"Text","type":"string"},"type":{"title":"Type","type":"string"}},"required":["type","text"],"title":"CommonChatSchemaTextContentPart","type":"object"},"CommonChatSchemaToolCall":{"properties":{"function":{"$ref":"#/components/schemas/CommonChatSchemaFunction"},"id":{"title":"Id","type":"string"},"type":{"title":"Type","type":"string"}},"required":["id","type","function"],"title":"CommonChatSchemaToolCall","type":"object"},"CommonChatSchemaToolMessage":{"properties":{"content":{"anyOf":[{"type":"string"},{"items":{"type":"string"},"type":"array"}],"title":"Content"},"role":{"title":"Role","type":"string"},"tool_call_id":{"title":"Tool Call Id","type":"string"}},"required":["role","content","tool_call_id"],"title":"CommonChatSchemaToolMessage","type":"object"},"CommonChatSchemaUserMessage":{"properties":{"content":{"anyOf":[{"type":"string"},{"items":{"anyOf":[{"$ref":"#/components/schemas/CommonChatSchemaTextContentPart"},{"$ref":"#/components/schemas/CommonChatSchemaImageContentPart"},{"$ref":"#/components/schemas/CommonChatSchemaAudioContentPart"}]},"type":"array"}],"title":"Content"},"name":{"title":"Name","type":"string"},"role":{"title":"Role","type":"string"}},"title":"CommonChatSchemaUserMessage","type":"object"},"HTTPValidationError":{"properties":{"detail":{"items":{"$ref":"#/components/schemas/ValidationError"},"title":"Detail","type":"array"}},"title":"HTTPValidationError","type":"object"},"Input":{"properties":{"messages":{"items":{"anyOf":[{"$ref":"#/components/schemas/CommonChatSchemaDeveloperMessage"},{"$ref":"#/components/schemas/CommonChatSchemaSystemMessage"},{"$ref":"#/components/schemas/CommonChatSchemaUserMessage"},{"$ref":"#/components/schemas/CommonChatSchemaAssistantMessage"},{"$ref":"#/components/schemas/CommonChatSchemaToolMessage"},{"$ref":"#/components/schemas/CommonChatSchemaFunctionMessage"}]},"title":"Messages","type":"array","x-order":0}},"required":["messages"],"title":"Input","type":"object"},"Output":{"title":"Output","type":"string"},"PredictionRequest":{"properties":{"created_at":{"format":"date-time","title":"Created At","type":"string"},"id":{"title":"Id","type":"string"},"input":{"$ref":"#/components/schemas/Input"},"output_file_prefix":{"title":"Output File Prefix","type":"string"},"webhook":{"format":"uri","maxLength":65536,"minLength":1,"title":"Webhook","type":"string"},"webhook_events_filter":{"default":["start","output","logs","completed"],"items":{"$ref":"#/components/schemas/WebhookEvent"},"type":"array"}},"title":"PredictionRequest","type":"object"},"PredictionResponse":{"properties":{"completed_at":{"format":"date-time","title":"Completed At","type":"string"},"created_at":{"format":"date-time","title":"Created At","type":"string"},"error":{"title":"Error","type":"string"},"id":{"title":"Id","type":"string"},"input":{"$ref":"#/components/schemas/Input"},"logs":{"default":"","title":"Logs","type":"string"},"metrics":{"title":"Metrics","type":"object"},"output":{"$ref":"#/components/schemas/Output"},"started_at":{"format":"date-time","title":"Started At","type":"string"},"status":{"$ref":"#/components/schemas/Status"},"version":{"title":"Version","type":"string"}},"title":"PredictionResponse","type":"object"},"Status":{"description":"An enumeration.","enum":["starting","processing","succeeded","canceled","failed"],"title":"Status","type":"string"},"ValidationError":{"properties":{"loc":{"items":{"anyOf":[{"type":"string"},{"type":"integer"}]},"title":"Location","type":"array"},"msg":{"title":"Message","type":"string"},"type":{"title":"Error Type","type":"string"}},"required":["loc","msg","type"],"title":"ValidationError","type":"object"},"WebhookEvent":{"description":"An enumeration.","enum":["start","output","logs","completed"],"title":"WebhookEvent","type":"string"}}},"info":{"title":"Cog","version":"0.1.0"},"openapi":"3.0.2","paths":{"/":{"get":{"operationId":"root__get","responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Root  Get"}}},"description":"Successful Response"}},"summary":"Root"}},"/health-check":{"get":{"operationId":"healthcheck_health_check_get","responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Healthcheck Health Check Get"}}},"description":"Successful Response"}},"summary":"Healthcheck"}},"/predictions":{"post":{"description":"Run a single prediction on the model","operationId":"predict_predictions_post","parameters":[{"in":"header","name":"prefer","required":false,"schema":{"title":"Prefer","type":"string"}}],"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PredictionRequest"}}}},"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PredictionResponse"}}},"description":"Successful Response"},"422":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/HTTPValidationError"}}},"description":"Validation Error"}},"summary":"Predict"}},"/predictions/{prediction_id}":{"put":{"description":"Run a single prediction on the model (idempotent creation).","operationId":"predict_idempotent_predictions__prediction_id__put","parameters":[{"in":"path","name":"prediction_id","required":true,"schema":{"title":"Prediction ID","type":"string"}},{"in":"header","name":"prefer","required":false,"schema":{"title":"Prefer","type":"string"}}],"requestBody":{"content":{"application/json":{"schema":{"allOf":[{"$ref":"#/components/schemas/PredictionRequest"}],"title":"Prediction Request"}}},"required":true},"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PredictionResponse"}}},"description":"Successful Response"},"422":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/HTTPValidationError"}}},"description":"Validation Error"}},"summary":"Predict Idempotent"}},"/predictions/{prediction_id}/cancel":{"post":{"description":"Cancel a running prediction","operationId":"cancel_predictions__prediction_id__cancel_post","parameters":[{"in":"path","name":"prediction_id","required":true,"schema":{"title":"Prediction ID","type":"string"}}],"responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Cancel Predictions  Prediction Id  Cancel Post"}}},"description":"Successful Response"},"422":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/HTTPValidationError"}}},"description":"Validation Error"}},"summary":"Cancel"}},"/shutdown":{"post":{"operationId":"start_shutdown_shutdown_post","responses":{"200":{"content":{"application/json":{"schema":{"title":"Response Start Shutdown Shutdown Post"}}},"description":"Successful Response"}},"summary":"Start Shutdown"}}}}`
	schema, err := openapi3.NewLoader().LoadFromData([]byte(openapiBody))
	require.NoError(t, err)
	inputs, err := NewInputs(keyVals, schema)
	require.NoError(t, err)
	require.Equal(t, expected, *inputs[key].ChatMessage)
}
