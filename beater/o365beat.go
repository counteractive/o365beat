package beater

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"

	// import o365beat-level processors (same style as filebeat)
	_ "github.com/elastic/beats/libbeat/processors/script"

	"github.com/counteractive/o365beat/config"
)

// authInfo holds information returned by the microsoft oauth API
// (see https://docs.microsoft.com/en-us/office/office-365-management-api/get-started-with-office-365-management-apis#sample-response)
type authInfo struct {
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
	expiresOn, _ := strconv.ParseInt(a.ExpiresOn, 10, 64)
	return time.Now().Unix() > (expiresOn - expirationBuffer)
}

// O365beat configuration and state.
type O365beat struct {
	done       chan struct{} // channel to initiate shutdown of main event loop
	config     config.Config // configuration settings
	client     beat.Client
	authURL    string // oauth authentication url built from config
	apiRootURL string // api root url built from config
	httpClient *http.Client
	auth       *authInfo
}

// New creates an instance of o365beat.
func New(b *beat.Beat, cfg *common.Config) (beat.Beater, error) {
	c := config.DefaultConfig
	if err := cfg.Unpack(&c); err != nil {
		err = fmt.Errorf("Error reading config file: %v", err)
		logp.Error(err)
		return nil, err
	}

	// using url.Parse seems like overkill
	loginURL := c.LoginURL
	resourceURL := c.ResourceURL
	au := loginURL + "/" + c.TenantDomain + "/oauth2/token?api-version=1.0"
	api := resourceURL + "/api/v1.0/" + c.DirectoryID + "/activity/feed/"
	cl := &http.Client{Timeout: c.APITimeout}
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
func (bt *O365beat) apiRequest(verb, urlStr string, body, query, headers map[string]string) (*http.Response, error) {
	reqBody := url.Values{}
	for k, v := range body {
		reqBody.Set(k, v)
	}
	req, err := http.NewRequest(verb, urlStr, strings.NewReader(reqBody.Encode()))
	if err != nil {
		logp.Error(err)
		return nil, err
	}
	reqQuery := req.URL.Query()                                // keep querystring values from urlStr
	reqQuery.Set("PublisherIdentifier", bt.config.DirectoryID) // to prevent throttling
	for k, v := range query {
		reqQuery.Set(k, v)
	}
	req.URL.RawQuery = reqQuery.Encode()
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// refresh authentication if expired
	if bt.auth == nil || bt.auth.expired() {
		logp.Info("auth nil or expired, re-authenticating")
		err = bt.authenticate()
		if err != nil {
			logp.Error(err)
			return nil, err
		}
	}
	req.Header.Set("Authorization", bt.auth.header())

	logp.Debug("api", "issuing api request: %s", req.URL.String())
	res, err := bt.httpClient.Do(req)
	if err != nil {
		logp.Error(err)
		return nil, err
	} else if res.StatusCode != 200 {
		// TODO: handle errors reading response body (previously overwritten by next line)
		body, _ := ioutil.ReadAll(res.Body)
		err = fmt.Errorf("non-200 status during api request.\n\tnewly enabled or newly subscribed feeds can take 12 hours or more to provide data.\n\tconfirm audit log searching is enabled for the target tenancy (https://docs.microsoft.com/en-us/microsoft-365/compliance/turn-audit-log-search-on-or-off#turn-on-audit-log-search).\n\treq: %v\n\tres: %v\n\t%v", req, res, string(body))
		logp.Error(err)
		return nil, err
	}
	return res, nil
}

// authenticate retrieves oauth2 information using client id and client_secret for use with the API
// https://docs.microsoft.com/en-us/azure/active-directory/develop/v2-oauth2-client-creds-grant-flow
func (bt *O365beat) authenticate() error {
	logp.Info("authenticating via %s", bt.authURL)
	reqBody := url.Values{}
	reqBody.Set("grant_type", "client_credentials")
	reqBody.Set("resource", bt.config.ResourceURL)
	reqBody.Set("client_id", bt.config.ClientID)
	reqBody.Set("client_secret", bt.config.ClientSecret)
	req, err := http.NewRequest("POST", bt.authURL, strings.NewReader(reqBody.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logp.Debug("auth", "sending auth req: %v", req)
	res, err := bt.httpClient.Do(req)
	if err != nil {
		logp.Error(err)
		return err
	} else if res.StatusCode != 200 {
		// TODO: handle errors reading response body:
		body, _ := ioutil.ReadAll(res.Body)
		err = fmt.Errorf("non-200 status during auth.\n\tcheck client secret and other config details.\n\treq: %v\n\tres: %v\n\t%v", req, res, string(body))
		logp.Error(err)
		return err
	}
	defer res.Body.Close()
	var ai authInfo
	json.NewDecoder(res.Body).Decode(&ai)
	logp.Debug("auth", "got auth info: %v", ai)
	bt.auth = &ai
	return nil
}

// listSubscriptions gets a collection of the current subscriptions and associated webhooks
// https://docs.microsoft.com/en-us/office/office-365-management-api/office-365-management-activity-api-reference#list-current-subscriptions
func (bt *O365beat) listSubscriptions() ([]map[string]string, error) {
	logp.Info("getting content subscriptions")
	logp.Debug("api", "getting content subscriptions from %v", bt.apiRootURL+"subscriptions/list")
	res, err := bt.apiRequest("GET", bt.apiRootURL+"subscriptions/list", nil, nil, nil)
	if err != nil {
		logp.Error(err)
		return nil, err
	}
	defer res.Body.Close()

	var subs []map[string]string
	json.NewDecoder(res.Body).Decode(&subs)
	logp.Debug("api", "got these subscriptions: %v", subs)
	return subs, nil
}

// subscribe starts a subscription to the specified content type
// https://docs.microsoft.com/en-us/office/office-365-management-api/office-365-management-activity-api-reference#start-a-subscription
func (bt *O365beat) subscribe(contentType string) (common.MapStr, error) {
	logp.Info("subscribing to content type %v", contentType)
	logp.Info("note that new subscriptions can take up to 12 hours to produce data")
	logp.Debug("api", "subscribing to %v at %s", contentType, bt.apiRootURL+"subscriptions/start")
	query := map[string]string{
		"contentType": contentType,
	}
	res, err := bt.apiRequest("POST", bt.apiRootURL+"subscriptions/start", nil, query, nil)
	if err != nil {
		logp.Error(err)
		return nil, err
	}
	defer res.Body.Close()

	var sub common.MapStr
	json.NewDecoder(res.Body).Decode(&sub)
	logp.Debug("api", "got this subscription response: %v", sub)
	return sub, nil
}

// enableSubscriptions enables subscriptions for all configured contentTypes
func (bt *O365beat) enableSubscriptions() error {
	logp.Info("enabling subscriptions for configured content types: %v", bt.config.ContentTypes)
	subscriptions, err := bt.listSubscriptions()
	if err != nil {
		logp.Error(err)
		return err
	}

	// add subscriptions as "disabled" if not in listSubscription results (can return []!):
	for _, t := range bt.config.ContentTypes {
		found := false
		for _, sub := range subscriptions {
			if sub["contentType"] == t {
				logp.Debug("api", "found subscription for contentType %s (enabled or disabled)", t)
				found = true
				break
			}
		}
		if !found {
			logp.Debug("api", "no subscription for configured contentType %s, appending to list to subscribe", t)
			subscriptions = append(subscriptions, map[string]string{"contentType": t, "status": "disabled"})
		}
	}

	for _, sub := range subscriptions {
		if sub["status"] != "enabled" {
			_, err := bt.subscribe(sub["contentType"])
			if err != nil {
				logp.Error(err)
				return err
			}
		}
	}
	return nil
}

// listAvailableContent gets blob locations for a single content type over <=24 hour span
// (the basic primitive provided by the API)
// https://docs.microsoft.com/en-us/office/office-365-management-api/office-365-management-activity-api-reference#list-available-content
func (bt *O365beat) listAvailableContent(contentType string, start, end time.Time) ([]map[string]string, error) {
	logp.Info("getting available content of type %s between %s and %s", contentType, start, end)
	logp.Debug(
		"api", "getting available content from %s of type %s between %s and %s",
		bt.apiRootURL+"subscriptions/content", contentType, start, end,
	)
	now := time.Now()
	if now.Sub(start) > bt.config.ContentMaxAge {
		logp.Warn("start (%v) must be <=%v hrs ago, resetting", start, bt.config.ContentMaxAge.Hours())
		start = now.Add(-bt.config.ContentMaxAge)
	}
	if end.Sub(start).Hours() > 24 {
		err := fmt.Errorf("start (%v) and end (%v) must be <=24 hrs apart", start, end)
		logp.Error(err)
		return nil, err
	}
	if end.Before(start) {
		err := fmt.Errorf("start (%v) must be before end (%v)", start, end)
		logp.Error(err)
		return nil, err
	}

	dateFmt := "2006-01-02T15:04:05" // API needs UTC in this format (no "Z" suffix)
	query := map[string]string{
		"contentType": contentType,
		"startTime":   start.UTC().Format(dateFmt),
		"endTime":     end.UTC().Format(dateFmt),
	}
	res, err := bt.apiRequest("GET", bt.apiRootURL+"subscriptions/content", nil, query, nil)
	if err != nil {
		logp.Error(err)
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
			logp.Error(err)
			return nil, err
		}
		json.NewDecoder(res.Body).Decode(&locs)
		res.Body.Close()
		contentList = append(contentList, locs...)
	}
	logp.Info(
		"got %v available content locations of type %s between %s and %s",
		len(contentList), contentType, start, end,
	)
	logp.Debug("api", "got this available content: %v", contentList)
	return contentList, nil
}

// listAllAvailableContent gets blob locations for multiple content types and spans up to 7 days
// sorted by contentCreated timestamp (uses the listAvailableContent function)
func (bt *O365beat) listAllAvailableContent(start, end time.Time) ([]map[string]string, error) {
	logp.Info("getting all available content between %s and %s", start, end)
	logp.Debug(
		"api", "getting all available content from %s between %s and %s",
		bt.apiRootURL+"subscriptions/content", start, end,
	)
	if end.Before(start) {
		err := fmt.Errorf("start (%v) must be before end (%v)", start, end)
		logp.Error(err)
		return nil, err
	}

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
				logp.Error(err)
				return nil, err
			}
			contentList = append(contentList, list...)
		}
		// could start downloads here if concurrency is implemented
		logp.Debug("api", "finished interval %v to %v", iStart, iEnd)
	}
	logp.Debug("api", "got these available content locations: %v", contentList)
	less := func(i, j int) bool {
		it, _ := time.Parse(time.RFC3339, contentList[i]["contentCreated"])
		jt, _ := time.Parse(time.RFC3339, contentList[j]["contentCreated"])
		return it.Before(jt)
	}
	sorted := sort.SliceIsSorted(contentList, less)
	if !sorted {
		logp.Debug("api", "available content locations were unsorted; sorting by creation time")
		sort.SliceStable(contentList, less)
	}
	return contentList, nil
}

// getContent gets actual content blobs
func (bt *O365beat) getContent(urlStr string) ([]common.MapStr, error) {
	logp.Debug("api", "getting content from %v.", urlStr)
	res, err := bt.apiRequest("GET", urlStr, nil, nil, nil)
	if err != nil {
		logp.Error(err)
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
		// event CreationTime needs "Z" appended (unlike blob contentCreated)
		ts, err := time.Parse(time.RFC3339, evt["CreationTime"].(string)+"Z")
		if err != nil {
			logp.Error(err)
			return err
		}
		fs := common.MapStr{}
		for k, v := range evt {
			fs[k] = v
		}
		beatEvent := beat.Event{Timestamp: ts, Fields: fs}
		bt.client.Publish(beatEvent)
	}
	return nil
}

func (bt *O365beat) poll(lastProcessed time.Time, b *beat.Beat) error {
	logp.Debug("beat", "polling since %v", lastProcessed)
	// start span just after last contentCreated or max bt.config.ContentMaxAge (default 7 days)
	now := time.Now()
	start := now.Add(-bt.config.ContentMaxAge)
	if start.Before(lastProcessed) {
		start = lastProcessed.Add(time.Second) // API granularity is by the second
	}

	// get all available content locations (sorted by contentCreated):
	availableContent, err := bt.listAllAvailableContent(start, now)
	if err != nil {
		err = fmt.Errorf("error listing all available content between %v and %v: %v", start, now, err)
		logp.Error(err)
		return err
	}

	// get the actual content and publish it (concurrently someday?)
	for _, v := range availableContent {
		content, err := bt.getContent(v["contentUri"])
		if err != nil {
			logp.Warn("error getting content: %v, moving to next blob", err)
			continue
		}
		err = bt.publish(content, b)
		if err != nil {
			logp.Error(err)
			return err
		}
		contentCreated, err := time.Parse(time.RFC3339, v["contentCreated"])
		if err != nil {
			logp.Error(err)
			return err
		}
		logp.Debug("beat", "published blob created %v, last was %v, updating registry", contentCreated, lastProcessed)
		err = bt.putRegistry(contentCreated)
		if err != nil {
			logp.Error(err)
			return err
		}
		lastProcessed = contentCreated
	}
	return nil
}

func (bt *O365beat) getRegistry() (time.Time, error) {
	logp.Debug("beat", "getting registry info from %v", bt.config.RegistryFilePath)
	reg, err := ioutil.ReadFile(bt.config.RegistryFilePath)
	if err != nil {
		logp.Warn("could not read registry file, may not exist (this is normal on first run). returning earliest possible time.")
		return time.Time{}, nil
	}
	lastProcessed, err := time.Parse(time.RFC3339, string(reg))
	if err != nil {
		// handle corrupted state file the same way we handle missing state file
		// (alternative: error out and let user try to fix state file)
		logp.Warn("error parsing timestamp in registry file (%v): %v; returning earliest possible time.", bt.config.RegistryFilePath, string(reg))
		return time.Time{}, nil
	}
	return lastProcessed, nil
}

func (bt *O365beat) putRegistry(lastProcessed time.Time) error {
	logp.Debug("beat", "putting registry info (%v) to %v", lastProcessed, bt.config.RegistryFilePath)
	ts := []byte(lastProcessed.Format(time.RFC3339))
	err := ioutil.WriteFile(bt.config.RegistryFilePath, ts, 0644)
	if err != nil {
		logp.Error(err)
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
		logp.Error(err)
		return err
	}
	ticker := time.NewTicker(bt.config.Period)

	err = bt.enableSubscriptions()
	if err != nil {
		logp.Error(err)
		return err
	}

	// registry (state) is just the most recent "contentCreated" for processed blobs
	// storing a timestamp means that blob and all before have been published.
	lastProcessed, err := bt.getRegistry()
	if err != nil {
		logp.Error(err)
		return err
	}

	// ticker's first tick is AFTER its period, do "initial tick" in advance:
	err = bt.poll(lastProcessed, b)
	if err != nil {
		logp.Error(err)
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
			logp.Error(err)
			return err
		}
		err = bt.poll(lastProcessed, b)
		if err != nil {
			logp.Error(err)
			return err
		}
	}
}

// Stop stops o365beat.
func (bt *O365beat) Stop() {
	bt.client.Close()
	close(bt.done)
}
