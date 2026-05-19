package whoop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"whoop-scraper/internal/auth"
)

const BaseURL = "https://api.prod.whoop.com/developer/v2"
const V1BaseURL = "https://api.prod.whoop.com/developer/v1"

type Client struct {
	auth       *auth.Client
	httpClient *http.Client
}

func NewClient(authClient *auth.Client, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	return &Client{auth: authClient, httpClient: httpClient}
}

func DateRange(days int) (string, string) {
	end := time.Now().In(time.Local)
	start := end.AddDate(0, 0, -days)
	return start.Format(time.DateOnly), end.Format(time.DateOnly)
}

func (c *Client) UserProfile(ctx context.Context) (map[string]any, error) {
	return c.get(ctx, "/user/profile/basic", nil)
}

func (c *Client) RevokeAccess(ctx context.Context) error {
	_, err := c.request(ctx, http.MethodDelete, BaseURL+"/user/access", nil)
	return err
}

func (c *Client) BodyMeasurement(ctx context.Context) (map[string]any, error) {
	return c.get(ctx, "/user/measurement/body", nil)
}

func (c *Client) ActivityV2UUID(ctx context.Context, activityV1ID string) (map[string]any, error) {
	return c.getURL(ctx, V1BaseURL+"/activity-mapping/"+url.PathEscape(activityV1ID), nil)
}

func (c *Client) CycleByID(ctx context.Context, cycleID string) (map[string]any, error) {
	return c.get(ctx, "/cycle/"+url.PathEscape(cycleID), nil)
}

func (c *Client) Cycles(ctx context.Context, startDate, endDate string) ([]map[string]any, error) {
	return c.getPaginated(ctx, "/cycle", startDate, endDate)
}

func (c *Client) SleepForCycle(ctx context.Context, cycleID string) (map[string]any, error) {
	return c.get(ctx, "/cycle/"+url.PathEscape(cycleID)+"/sleep", nil)
}

func (c *Client) RecoveryForCycle(ctx context.Context, cycleID string) (map[string]any, error) {
	return c.get(ctx, "/cycle/"+url.PathEscape(cycleID)+"/recovery", nil)
}

func (c *Client) Recovery(ctx context.Context, startDate, endDate string) ([]map[string]any, error) {
	return c.getPaginated(ctx, "/recovery", startDate, endDate)
}

func (c *Client) SleepByID(ctx context.Context, sleepID string) (map[string]any, error) {
	return c.get(ctx, "/activity/sleep/"+url.PathEscape(sleepID), nil)
}

func (c *Client) Sleep(ctx context.Context, startDate, endDate string) ([]map[string]any, error) {
	return c.getPaginated(ctx, "/activity/sleep", startDate, endDate)
}

func (c *Client) WorkoutByID(ctx context.Context, workoutID string) (map[string]any, error) {
	return c.get(ctx, "/activity/workout/"+url.PathEscape(workoutID), nil)
}

func (c *Client) Workouts(ctx context.Context, startDate, endDate string) ([]map[string]any, error) {
	return c.getPaginated(ctx, "/activity/workout", startDate, endDate)
}

func (c *Client) getPaginated(ctx context.Context, endpoint, startDate, endDate string) ([]map[string]any, error) {
	var records []map[string]any
	nextToken := ""
	for {
		params := url.Values{}
		params.Set("limit", "25")
		params.Set("start", startDate+"T00:00:00.000Z")
		params.Set("end", endDate+"T23:59:59.999Z")
		if nextToken != "" {
			params.Set("nextToken", nextToken)
		}
		data, err := c.get(ctx, endpoint, params)
		if err != nil {
			return nil, err
		}
		pageRecords, _ := data["records"].([]any)
		for _, item := range pageRecords {
			if record, ok := item.(map[string]any); ok {
				records = append(records, record)
			}
		}
		if value, _ := data["next_token"].(string); value != "" {
			nextToken = value
			continue
		}
		return records, nil
	}
}

func (c *Client) get(ctx context.Context, endpoint string, params url.Values) (map[string]any, error) {
	return c.getURL(ctx, BaseURL+endpoint, params)
}

func (c *Client) getURL(ctx context.Context, requestURL string, params url.Values) (map[string]any, error) {
	return c.request(ctx, http.MethodGet, requestURL, params)
}

func (c *Client) request(ctx context.Context, method, requestURL string, params url.Values) (map[string]any, error) {
	accessToken, err := c.auth.ValidAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	if len(params) > 0 {
		requestURL += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("whoop api %s %s failed: %s", method, requestURL, resp.Status)
	}
	if resp.StatusCode == http.StatusNoContent {
		return map[string]any{}, nil
	}
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}
