package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_Caching(t *testing.T) {
	assert := assert.New(t)
	//enable the cache flag
	*cacheEnabled = true
	// Start the server
	go main()
	time.Sleep(2 * time.Second)

	start := time.Now()
	res, err := http.Get("http://localhost:9042/scrape?task=billing")
	nonCachedDuration := time.Since(start)
	assert.Equal(http.StatusOK, res.StatusCode, "Request status not OK")
	assert.Nil(err, "No errors should be thrown")

	// Responses here should come from cache
	cumalitiveRequestTime := time.Duration(0)
	for i := 0; i < 10; i++ {
		start = time.Now()
		res, err = http.Get("http://localhost:9042/scrape?task=billing")
		cachedDuration := time.Since(start)
		assert.Equal(http.StatusOK, res.StatusCode, "Request status not OK")
		assert.Nil(err, "No errors should be thrown")
		cumalitiveRequestTime = cumalitiveRequestTime + cachedDuration
		time.Sleep(1 * time.Second)
	}
	avgCachedDuration := cumalitiveRequestTime / 10

	fmt.Println("Sleeping till cache expires")
	time.Sleep(60 * time.Second)
	start = time.Now()
	res, err = http.Get("http://localhost:9042/scrape?task=billing")
	nonCachedDuration2 := time.Since(start)
	assert.Equal(http.StatusOK, res.StatusCode, "Request status not OK")
	assert.Nil(err, "No errors should be thrown")

	// asserting that cache is being used:
	assert.True(avgCachedDuration < nonCachedDuration, "Cached requests duration longer than expected")
	assert.True(avgCachedDuration < nonCachedDuration2, "Cached requests duration longer than expected")

	// cached request should take less than half the time of non cached ones
	assert.True(avgCachedDuration < nonCachedDuration/2, "Cached requests duration longer than expected")
	assert.True(avgCachedDuration < nonCachedDuration2/2, "Cached requests duration longer than expected")
}
