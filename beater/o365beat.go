package beater

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"

	"github.com/counteractive/o365beat/config"
)

type authInfo struct {
	// example: {"token_type":"Bearer","expires_in":"3599","expires_on":"1431659094",
	//           "not_before":"1431655194","resource":"https://manage.office.com",
	//           "access_token":"eyJ0eXAiOiJKV1QiL..."}
	TokenType   string `json:"token_type"`
	ExpiresIn   string `json:"expires_in"`
	ExpiresOn   string `json:"expires_on"`
	NotBefore   string `json:"not_before"`
	Resource    string `json:"resource"`
	AccessToken string `json:"access_token"`
}

func (a *authInfo) header() string {
	return fmt.Sprintf("%s %s", a.TokenType, a.AccessToken)
}

func (a *authInfo) expired() bool {
	const expirationBuffer = 60 // extra seconds unexpired token is considered expired
	expiration, _ := strconv.ParseInt(a.ExpiresOn, 10, 64)
	return time.Now().Unix() > (expiration - expirationBuffer)
}

// O365beat configuration.
type O365beat struct {
	done       chan struct{} // channel to initiate shutdown of main event loop
	config     config.Config // configuration settings
	client     beat.Client
	authURL    string // oauth2 authentication url built from config
	apiRootURL string // api root url built from config
	httpClient *http.Client
	auth       *authInfo
}

// New creates an instance of o365beat.
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	c := config.DefaultConfig
	if err := cfg.Unpack(&c); err != nil {
		return nil, fmt.Errorf("Error reading config file: %v", err)
	}

	// using url.Parse seems like overkill
	loginURL := "https://login.microsoftonline.com/"
	resourceURL := "https://manage.office.com/"
	au := loginURL + c.TenantDomain + "/oauth2/token?api-version=1.0"
	api := resourceURL + "api/v1.0/" + c.DirectoryID + "/activity/feed/"
	cl := &http.Client{Timeout: time.Second * 30}
	var ai *authInfo

	bt := &O365beat{
		done:       make(chan struct{}),
		config:     c,
		authURL:    au,
		apiRootURL: api,
		httpClient: cl,
		auth:       ai,
	}
	return bt, nil
}

// apiRequest issues an http request with api authorization header
func (bt *O365beat) apiRequest(
	method, urlStr string, body, query, headers map[string]string,
) (*http.Response, error) {
	reqBody := url.Values{}
	for k, v := range body {
		reqBody.Set(k, v)
	}
	req, err := http.NewRequest(method, urlStr, strings.NewReader(reqBody.Encode()))
	if err != nil {
		return nil, err
	}
	reqQuery := req.URL.Query() // keep existing querystring values from urlStr
	for k, v := range query {
		reqQuery.Set(k, v)
	}
	req.URL.RawQuery = reqQuery.Encode()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// refresh authentication if expired (authenticate() doesn't use this func)
	if bt.auth == nil || bt.auth.expired() {
		logp.Info("auth nil or expired, re-authenticating")
		err = bt.authenticate()
		if err != nil {
			return nil, err
		}
	}
	req.Header.Set("Authorization", bt.auth.header())

	logp.Debug("http", "issuing request: %s", req.URL.String())
	res, err := bt.httpClient.Do(req)
	if err != nil {
		return nil, err
	} else if res.StatusCode != 200 {
		return nil, fmt.Errorf("non-200 status code. req:\n\t%v\n\tres:\n\t%v", req, res)
	}
	return res, nil
}

func (bt *O365beat) authenticate() error {
	// does not use apiRequest helper to allow clean use of this func therein
	logp.Info("authenticating via: %s", bt.authURL)
	reqBody := url.Values{}
	reqBody.Set("grant_type", "client_credentials")
	reqBody.Set("resource", "https://manage.office.com")
	reqBody.Set("client_id", bt.config.ClientID)
	reqBody.Set("client_secret", bt.config.ClientSecret)
	req, err := http.NewRequest("POST", bt.authURL, strings.NewReader(reqBody.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logp.Debug("auth", "sending auth req: %v", req)
	res, err := bt.httpClient.Do(req)
	if err != nil {
		return err
	} else if res.StatusCode != 200 {
		return fmt.Errorf("non-200 status code during auth (see below).\n\treq: %v\n\tres:\n\t%v", req, res)
	}
	defer res.Body.Close()
	var ai authInfo
	json.NewDecoder(res.Body).Decode(&ai)
	logp.Debug("auth", "got authentication information: %v", ai)
	bt.auth = &ai
	return nil
}

// listSubscriptions gets a collection of the current subscriptions and associated webhooks
// https://docs.microsoft.com/en-us/office/office-365-management-api/office-365-management-activity-api-reference#list-current-subscriptions
func (bt *O365beat) listSubscriptions() ([]map[string]string, error) {
	logp.Info("getting subscriptions from: %s", bt.apiRootURL+"subscriptions/list")
	query := map[string]string{"PublisherIdentifier": bt.config.DirectoryID}
	res, err := bt.apiRequest("GET", bt.apiRootURL+"subscriptions/list", nil, query, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var subs []map[string]string // expect an arbitrary json array
	json.NewDecoder(res.Body).Decode(&subs)
	logp.Debug("api", "got the following subscriptions:\n\t%v", subs)
	return subs, nil
}

// subscribe starts a subscription to the specified content type
// https://docs.microsoft.com/en-us/office/office-365-management-api/office-365-management-activity-api-reference#start-a-subscription
func (bt *O365beat) subscribe(contentType string) (common.MapStr, error) {
	logp.Info("subscribing with the following URL: %s", bt.apiRootURL+"subscriptions/start")
	query := map[string]string{
		"contentType":         contentType,
		"PublisherIdentifier": bt.config.DirectoryID,
	}
	res, err := bt.apiRequest("POST", bt.apiRootURL+"subscriptions/start", nil, query, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var sub common.MapStr
	json.NewDecoder(res.Body).Decode(&sub)
	logp.Debug("api", "got the following subscription response:\n\t%v", sub)
	return sub, nil
}

const maxAgeMinutes = (7 * 24 * 60)

// listAvailableContent gets blob locations for a single content type over a span <=24 hours
// (the basic primitive provided by the API)
// https://docs.microsoft.com/en-us/office/office-365-management-api/office-365-management-activity-api-reference#list-available-content
func (bt *O365beat) listAvailableContent(
	contentType string, start, end time.Time,
) ([]map[string]string, error) {
	logp.Info(
		"getting available content locations from %s for content type %s between %s and %s",
		bt.apiRootURL+"subscriptions/content", contentType,
		start.Format(time.RFC3339), end.Format(time.RFC3339),
	)
	now := time.Now()
	if now.Sub(start).Minutes() > maxAgeMinutes {
		logp.Warn("start time can't be more than %v minutes ago, resetting.", maxAgeMinutes)
		start = now.Add(-maxAgeMinutes * time.Minute)
	}
	if end.Sub(start).Hours() > 24 {
		return nil, fmt.Errorf("start and end times must be at most 24 hrs apart")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("end time cannot be before start time")
	}

	dateFmt := "2006-01-02T15:04:05" // API needs UTC in this format (no "Z" suffix)
	query := map[string]string{
		"contentType":         contentType,
		"startTime":           start.UTC().Format(dateFmt),
		"endTime":             end.UTC().Format(dateFmt),
		"PublisherIdentifier": bt.config.DirectoryID,
	}
	res, err := bt.apiRequest("GET", bt.apiRootURL+"subscriptions/content", nil, query, nil)
	if err != nil {
		return nil, err
	}

	var locs []map[string]string
	json.NewDecoder(res.Body).Decode(&locs)
	res.Body.Close()
	contentList := locs

	for res.Header.Get("NextPageUri") != "" {
		next := res.Header.Get("NextPageUri")
		logp.Debug("api", "getting next page of results from NextPageUri: %v", next)
		res, err = bt.apiRequest("GET", next, nil, nil, nil) // don't redeclare res!
		if err != nil {
			return nil, err
		}
		json.NewDecoder(res.Body).Decode(&locs)
		res.Body.Close()
		contentList = append(contentList, locs...)
	}

	logp.Debug("api", "got the following available content locations:\n\t%v", contentList)
	return contentList, nil
}

// listAllAvailableContent gets blob locations for multiple content types and spans up to 7 days
// (uses the listAvailableContent function)
func (bt *O365beat) listAllAvailableContent(start, end time.Time) ([]map[string]string, error) {
	logp.Info(
		"getting all available data locations from %s between %s and %s",
		bt.apiRootURL+"subscriptions/content",
		start.Format(time.RFC3339), end.Format(time.RFC3339),
	)
	now := time.Now()
	if now.Sub(start).Minutes() > maxAgeMinutes {
		logp.Warn("start time can't be more than %v minutes ago, resetting.", maxAgeMinutes)
		start = now.Add(-maxAgeMinutes * time.Minute)
	}
	if end.Before(start) {
		return nil, fmt.Errorf("end time cannot be before start time")
	}
	// TODO: consider checking if end is nil and default to time.Now()

	interval := 24 * time.Hour
	var contentList []map[string]string

	// loop through intervals:
	for iStart, iEnd := start, start; iStart.Before(end); iStart = iEnd {
		iEnd = iStart.Add(interval)
		if end.Before(iEnd) {
			iEnd = end
		}

		// loop through all content types this interval:
		for _, t := range bt.config.ContentTypes {
			list, err := bt.listAvailableContent(t, iStart, iEnd)
			if err != nil {
				return nil, err
			}
			contentList = append(contentList, list...)
		}
		logp.Debug("loop", "finished interval between %v and %v (could begin downloads)",
			iStart.Format(time.RFC3339), iEnd.Format(time.RFC3339))
	}
	logp.Debug("api", "got the following available content locations:\n\t%v", contentList)
	return contentList, nil
}

func (bt *O365beat) getContent(urlStr string) ([]common.MapStr, error) {
	logp.Debug("api", "getting content from %v.", urlStr)
	res, err := bt.apiRequest("GET", urlStr, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	var events []common.MapStr
	json.NewDecoder(res.Body).Decode(&events)
	res.Body.Close()
	return events, nil
}

// publish sends events into the beats pipeline
func (bt *O365beat) publish(content []common.MapStr, b *beat.Beat) error {
	logp.Debug("beat", "publishing %v event(s)", len(content))
	for _, evt := range content {
		ts, err := time.Parse(time.RFC3339, evt["CreationTime"].(string)+"Z")
		// ts, err := time.Parse(time.RFC3339, evt["CreationTime"].(string))
		if err != nil {
			return err
		}
		fs := common.MapStr{}
		for k, v := range evt {
			fs[k] = v
		}
		fs["type"] = b.Info.Name
		beatEvent := beat.Event{Timestamp: ts, Fields: fs}
		bt.client.Publish(beatEvent)
	}
	return nil
}

func (bt *O365beat) poll(lastProcessed time.Time, b *beat.Beat) error {
	logp.Debug("poll", "polling since %v", lastProcessed)
	// set start of span to earlier of last contentCreated or maxAgeMinutes (7 days)
	now := time.Now()
	start := now.Add(-maxAgeMinutes * time.Minute)
	if start.Before(lastProcessed) {
		start = lastProcessed.Add(time.Second) // API granularity is by the second
	}

	// get all available content locations:
	availableContent, err := bt.listAllAvailableContent(start, now)
	if err != nil {
		logp.Warn(
			"error retrieving all available content between %v and %v:\n\t%v\ncontinuing.",
			start.Format(time.RFC3339), now.Format(time.RFC3339), err,
		)
	}

	// get the actual content and publish it
	for _, v := range availableContent {
		// TODO: consider doing this concurrently:
		content, err := bt.getContent(v["contentUri"])
		if err != nil {
			logp.Warn("error getting content: %v, moving to next blob", err)
			continue
		}
		err = bt.publish(content, b)
		if err != nil {
			return err
		}
		contentCreated, err := time.Parse(time.RFC3339, v["contentCreated"]) // why doesn't this need a Z?
		if err != nil {
			return err
		}
		logp.Debug("poll", "published with contentCreated of %v, lastProcessed is %v", contentCreated, lastProcessed)
		if lastProcessed.Before(contentCreated) {
			err := bt.putRegistry(contentCreated)
			if err != nil {
				return err
			}
			lastProcessed = contentCreated
		}
	}
	return nil
}

func (bt *O365beat) getRegistry() (time.Time, error) {
	logp.Debug("reg", "getting registry info from %v", bt.config.RegistryFilePath)
	reg, err := ioutil.ReadFile(bt.config.RegistryFilePath)
	if err != nil {
		logp.Warn("error parsing registry file, may not exist.")
		return time.Time{}, nil
	}
	lastProcessed, err := time.Parse(time.RFC3339, string(reg))
	if err != nil {
		logp.Warn("error parsing lastProcessed timestamp from registry file")
		return lastProcessed, err
	}
	return lastProcessed, nil
}

func (bt *O365beat) putRegistry(lastProcessed time.Time) error {
	logp.Debug("reg", "putting registry info (%v) to %v", lastProcessed, bt.config.RegistryFilePath)
	ts := []byte(lastProcessed.Format(time.RFC3339))
	err := ioutil.WriteFile(bt.config.RegistryFilePath, ts, 0644)
	if err != nil {
		return err
	}
	return nil
}

// Run starts o365beat.
func (bt *O365beat) Run(b *beat.Beat) error {
	logp.Info("o365beat is running! Hit CTRL-C to stop it.")

	var err error
	bt.client, err = b.Publisher.Connect()
	if err != nil {
		return err
	}
	ticker := time.NewTicker(bt.config.Period)

	// enable all subscriptions
	subscriptions, err := bt.listSubscriptions()
	for _, sub := range subscriptions {
		if sub["status"] != "enabled" {
			_, err := bt.subscribe(sub["contentType"])
			if err != nil {
				return err
			}
		}
	}

	// registry (state) is just the most recent "contentCreated" for processed blobs
	// storing a timestamp means that blob and all before have been published.
	lastProcessed, err := bt.getRegistry()
	if err != nil {
		return err
	}

	// ticker's first tick is AFTER its period, do "initial tick" in advance:
	err = bt.poll(lastProcessed, b)
	if err != nil {
		return err
	}

	// api polling loop:
	for {
		select {
		case <-bt.done:
			return nil
		case <-ticker.C:
		}
		lastProcessed, err := bt.getRegistry()
		if err != nil {
			return err
		}
		bt.poll(lastProcessed, b)
		if err != nil {
			return err
		}
	}
}

// Stop stops o365beat.
func (bt *O365beat) Stop() {
	bt.client.Close()
	close(bt.done)
}
