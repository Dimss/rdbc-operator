package rdbc

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type RedisDb struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	MemorySize int    `json:"memory_size"`
	uid        float64
	endpoint   string
}

func init() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
}

type RedisDbApi struct {
	Response map[string]interface{} `json:"-"`
}

func NewRedisDb(dbName string, size int) *RedisDb {
	req := new(RedisDb)
	req.Name = dbName
	// As for now, the Reids DB operator will support only Redis DB creation
	req.Type = "redis"
	// User set DB size in Megabytes, API uses memory size in Kilobytes
	req.MemorySize = size * 1024
	return req
}

func (rdb *RedisDb) CreateDb(redisConfig RedisConfig) error {

	url := redisConfig.APIUrl + "/v1/bdbs"
	client := &http.Client{}
	b, err := json.Marshal(rdb)
	if err != nil {
		return fmt.Errorf("failed to Marshal RedisDb for DB request: %s", rdb.Name)
	}
	log.Info(fmt.Sprintf("create DB Url: %s", url))
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(b))
	if err != nil {
		return fmt.Errorf("failed compose new POST request for url: %s", url)
	}
	req.SetBasicAuth(redisConfig.Username, redisConfig.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err, "failed execute request POST request ")
		return fmt.Errorf("failed execute POST request for url: %s", url)
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode > 299 {
		return fmt.Errorf("bad status code: %d, body: %s", resp.StatusCode, bodyText)
	}
	redisDBCreateResponse := RedisDbApi{}
	err = json.Unmarshal([]byte(string(bodyText)), &redisDBCreateResponse.Response)
	if err != nil {
		return fmt.Errorf("error while unmarshalling response")
	}
	if n, ok := redisDBCreateResponse.Response["uid"].(float64); ok {
		rdb.uid = n
	} else {
		return fmt.Errorf("error while getting UID from response")
	}
	return nil
}

func (rdb *RedisDb) GetDb(redisConfig RedisConfig) error {
	if rdb.uid == 0 {
		return fmt.Errorf("uid is not set, can't get db details for db: %s", rdb.Name)
	}
	url := fmt.Sprintf("%v/v1/bdbs/%v", redisConfig.APIUrl, rdb.uid)
	log.Info(fmt.Sprintf("get DB Url: %s", url))
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(redisConfig.Username, redisConfig.Password)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to executed GET request for url: %s", url)
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode > 299 {
		return fmt.Errorf("bad status code: %d, body: %s", resp.StatusCode, bodyText)
	}
	redisDbApi := RedisDbApi{}
	err = json.Unmarshal([]byte(string(bodyText)), &redisDbApi.Response)
	if err != nil {
		return fmt.Errorf("error while unmarshalling response")
	}
	if endpoints, ok := redisDbApi.Response["endpoints"].([]interface{}); ok {
		if len(endpoints) < 1 {
			return fmt.Errorf("enpoints list is empty for DB: %v", rdb.uid)
		}
		ep := endpoints[0].(map[string]interface{})
		rdb.endpoint = fmt.Sprintf("%s:%v", ep["dns_name"].(string), ep["port"].(float64))
		log.Info(fmt.Sprintf("db endpoint %s", rdb.endpoint))
	} else {
		return fmt.Errorf("error while getting endpoints from response")
	}
	return nil
}
