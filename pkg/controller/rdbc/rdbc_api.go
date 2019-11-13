package rdbc

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"time"
	"github.com/google/uuid"
)

type RedisDb struct {
	Uid        int32  `json:"uid"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	MemorySize int    `json:"memory_size"`
	Password   string `json:"authentication_redis_pass"`
	endpoint   string
}

func init() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
}

type RedisDbApi struct {
	Response map[string]interface{} `json:"-"`
}

func NewRedisDb(dbName string, size int, password string, redis *RedisConfig) (*RedisDb, error) {
	db := new(RedisDb)
	dbId, err := redis.getUniqDbId()
	if err != nil {
		log.Error(err, "wasn't able to generate unique BD ID")
		return nil, err
	}
	// Set password if it's not set by user
	if password == "" {
		passUuid, err := uuid.NewUUID()
		if err != nil {
			log.Error(err, "failed to generate password")
			return nil, err
		}
		password = passUuid.String()[0:8]
	}
	db.Uid = dbId
	db.Name = dbName
	db.Password = password
	// As for now, the Reids DB operator will support only Redis DB creation
	db.Type = "redis"
	// User set DB size in Megabytes, API uses memory size in bytes
	db.MemorySize = size * 1024 * 1024
	return db, nil
}

func (redis *RedisConfig) LoadRedisDb(dbId int32) (*RedisDb, error) {
	rdb := &RedisDb{Uid: dbId}
	err := redis.GetDb(rdb)
	if err != nil {
		log.Error(err, fmt.Sprintf("wasn't able to get db, dbid: %d", dbId))
		return nil, err
	}
	return rdb, nil
}

func (redis *RedisConfig) getUniqDbId() (int32, error) {
	// Init random
	rand.Seed(time.Now().UnixNano())
	// Will try to generate 3 times uniq ID for DB
	// and check if the DB with generated random ID exists
	// Redis DBID is an integer: https://storage.googleapis.com/rlecrestapi/rest-html/http_rest_api.html#post--v1-bdbs
	// thus, I can't be sure that while generating a new random ID it will unique,
	// maybe a DB with such ID already exists,
	// so I'll generate 3 times random ID and check if such a DB already exists
	for i := 0; i < 3; i++ {
		dbId := rand.Int31()
		exists, err := redis.CheckIfDbExists(dbId)
		if err != nil {
			// Error during getting the DB, will stop the loop and return error
			log.Error(err, "error checking dbid uniqueness")
			return 0, err
		}
		if !exists {
			// All good, db with dbid doesn't exists
			log.Info(fmt.Sprintf("The DB with dbid: %v does not exists, will proceed with DB creation", dbId))
			return dbId, nil
		}
	}

	return 0, fmt.Errorf("wasn't able to genereate unique DB ID 3 times... ")

}

func (redis *RedisConfig) CheckIfDbExists(dbId int32) (bool, error) {
	resp, err := redis.execApiRequest(fmt.Sprintf("%v/v1/bdbs/%v", redis.APIUrl, dbId), "GET", nil)
	if err != nil {
		return false, err
	}
	if resp.StatusCode == 404 {
		return false, nil
	}
	return true, nil
}

func (redis *RedisConfig) CreateDb(rdb *RedisDb) error {
	// Compose URL
	url := redis.APIUrl + "/v1/bdbs"
	// Marshal request body
	b, err := json.Marshal(rdb)
	if err != nil {
		return fmt.Errorf("failed to Marshal RedisDb for DB request: %s", rdb.Name)
	}
	// Exec request and get response
	resp, err := redis.execApiRequest(url, "POST", b)
	if err != nil {
		log.Error(err, "failed execute request POST request ")
		return fmt.Errorf("failed execute POST request for url: %s", url)
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	// Assume any code bellow 299 is valid
	if resp.StatusCode > 299 {
		return fmt.Errorf("bad status code: %d, body: %s", resp.StatusCode, bodyText)
	}
	return nil
}

func (redis *RedisConfig) GetDb(rdb *RedisDb) error {

	// Compose URL
	url := fmt.Sprintf("%v/v1/bdbs/%v", redis.APIUrl, rdb.Uid)
	// Exec request
	resp, err := redis.execApiRequest(url, "GET", nil)

	if err != nil {
		return fmt.Errorf("failed to executed GET request for url: %s", url)
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	// Assume any code bellow 299 is valid
	if resp.StatusCode > 299 {
		return fmt.Errorf("bad status code: %d, body: %s", resp.StatusCode, bodyText)
	}
	redisDbApi := RedisDbApi{}
	err = json.Unmarshal([]byte(string(bodyText)), &redisDbApi.Response)
	if err != nil {
		return fmt.Errorf("error while unmarshalling response")
	}
	rdb.Name = redisDbApi.Response["name"].(string)
	rdb.MemorySize = int(redisDbApi.Response["memory_size"].(float64) / 1024 / 1024)
	rdb.Type = redisDbApi.Response["type"].(string)
	rdb.Password = redisDbApi.Response["authentication_redis_pass"].(string)
	if endpoints, ok := redisDbApi.Response["endpoints"].([]interface{}); ok {
		if len(endpoints) < 1 {
			return fmt.Errorf("enpoints list is empty for DB: %v", rdb.Uid)
		}
		ep := endpoints[0].(map[string]interface{})
		rdb.endpoint = fmt.Sprintf("%s:%v", ep["dns_name"].(string), ep["port"].(float64))
		log.Info(fmt.Sprintf("db endpoint %s", rdb.endpoint))
	} else {
		return fmt.Errorf("error while getting endpoints from response")
	}
	return nil
}

func (redis *RedisConfig) execApiRequest(url string, method string, body []byte) (response *http.Response, err error) {
	log.Info(fmt.Sprintf("get DB Url: %s", url))
	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		log.Error(err, "Error during composing new http request", "url", url)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Set Auth
	req.SetBasicAuth(redis.Username, redis.Password)
	// Exec request
	response, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to executed %s request for url: %s", method, url)
	}
	return
}

func (redis *RedisConfig) DeleteDb(rdb *RedisDb) error {

	// Check if DB exists in the cluster

	dBExists, err := redis.CheckIfDbExists(rdb.Uid)
	if !dBExists {
		log.Error(err, fmt.Sprintf("db: %s doesn't exists in cluster, asuming it was deleted manually", rdb.Name))
		// if db not exists, assume it was delete manually,
		// and proceed normally with the request
		return nil
	}
	// Compose URL
	url := fmt.Sprintf("%v/v1/bdbs/%v", redis.APIUrl, rdb.Uid)
	resp, err := redis.execApiRequest(url, "DELETE", nil)
	if err != nil {
		return fmt.Errorf("failed to executed DELETE request for url: %s", url)
	}
	// Assume any code bellow 299 is valid
	if resp.StatusCode > 299 {
		return fmt.Errorf("bad status code: %d, for DELETE request: %s", resp.StatusCode, url)
	}
	return nil
}
