# Neo4J Aura Go Wrapper
This is a Go wrapper for the Neo4j Aura API. Neo4j Aura is a fully-managed cloud database service provided by Neo4j, allowing users to deploy and manage Neo4j databases in the cloud without operational complexities.

## Installatation

## Usage
Interacting with the Aura API is done through a client initialized through your credentials and you tenant ID. Details on how to create credentials can be found in [the Aura documentation](https://neo4j.com/docs/aura/platform/api/authentication/) and the tenant ID can be found in the [Aura console](https://console.neo4j.io/).
```
package main

import (
	"fmt"
	"github.com/indykite/aura-api-client/aura"
)

func main() {
	// Initialize Neo4j Aura client
	clientID := "your-client-id"
	tenantID := "your-tenant-id"
	clientSecret := "your-client-secret"
	wrapper := aura.NewClient(clientID, tenantID, clientSecret)
    // ... do stuff with the client here
}
```
Optionally the client can be extended using options functions supplied to the constructor. The available functionality can be found in the library itself.
```
wrapper = aura.NewClient(clientID, tenantID, clientSecret, 
    WithHTTPClient(customHTTPClient),
    WithRetries(2),
)
```
### Creating an instance
When creating an instance the instance configuration must be provided.
```
// Create Neo4j Aura instance
instanceName := "my-instance"
cloudProvider := "gcp"
memory := "2GB"
version := "5"
region := "us-east-1"
instanceType := "enterprise-db"

createResponse, err := wrapper.CreateInstance(instanceName, cloudProvider, memory, version, region, instanceType)
if err != nil {
    fmt.Println("Error creating Neo4j Aura instance:", err)
}

fmt.Printf("Instance created successfully. ID: %s\n", createResponse.ID)
```
The response from the call to `CreateInstance` contains instance ID, initial credentials, connection URL along with your tenant id, cloud provider, region, instance type, and the instance name for you to use once the instance is running. It is important to store these initial credentials until you have the chance to login to your running instance and change them. 
Note that spinning up an instance might take some time and you will know that the instance is ready when its status switches from `creating` to `running`.
### Getting instance information
The state of an instance can be found using the ID returned from creating the instance.
```
instanceID := "aura-generated-UUID"

getResponse, err := wrapper.GetInstance(instanceID)
if err != nil {
    fmt.Println("Error getting Neo4j Aura instance:", err)
}
fmt.Println("Current instance state is: "+getResponse.Status)
```
### Destroying an instance
An already running instance can be destroyed through the API using the ID returned from creating the instance.
```
instanceID := "aura-generated-UUID"

err := wrapper.DestroyInstance(instanceID)
if err != nil {
    fmt.Println("Error destroying Neo4j Aura instance:", err)
}
```
If the instance already has been destroyed the API will return a 404, which the wrapper treats as a success to make the operation idempotent.
## Configuration
### Custom HTTP clients
By default the wrapper uses `http.Client`, but a custom client can be provided to the constructor
```
betterHttp, _ := some.SpecialHttpClient{}
wrapper = aura.NewClient(clientID, tenantID, clientSecret,
    betterHttp)
```
### Retrying operations
The Aura API recommends retrying failing operations with codes 500, 502, 503 and 504. By default these will be returned as errors, but the client can be set to retry up to 3 times when encouting these status codes.
```
wrapper = aura.NewClient(clientID, tenantID, clientSecret,
    aura.WithRetries(2))
```
When this has been set any operation returning a 500, 502, 503 and 504 will have its response logged and retried after some backoff.
### Logging
By default logging is done using the standard `slog`, but a custom logger can be provided to the constructor
```
logger, _ := zap.NewProduction()
wrapper = aura.NewClient(clientID, tenantID, clientSecret,
    logger)
```
### Deprecation warning
Neo4J adds a header to the responses if the API has been deprecated. When encountered the API wrapper will issue a warning through the logger detailing the deprecation date and the URL where it was encountered.
### API versioning
If a new Aura API is added in the future it can be selected using 
```
wrapper = aura.NewClient(clientID, tenantID, clientSecret,
    aura.WithVersion("v2"))
```