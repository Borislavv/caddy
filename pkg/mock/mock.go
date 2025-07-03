package mock

import (
	"context"
	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/caddyserver/caddy/v2/pkg/model"
	"net/http"
	"strconv"
)

// GenerateRandomRequests produces a slice of *model.Request for use in tests and benchmarks.
// Each request gets a unique combination of project, domain, language, and tags.
func GenerateRandomRequests(cfg *config.Cache, path []byte, num int) []*model.Request {
	i := 0
	list := make([]*model.Request, 0, num)

	// Iterate over all possible language and project ID combinations until num requests are created
	for {
		//for _, lng := range localesandlanguages.LanguageCodeList() {
		//for projectID := 1; projectID < 1000; projectID++ {
		if i >= num {
			return list
		}
		req := model.NewRequest(
			cfg,
			path,
			[][2][]byte{
				{[]byte("project[id]"), []byte("285")},
				{[]byte("domain"), []byte("1x001.com")},
				{[]byte("language"), []byte("en")},
				{[]byte("choice[name]"), []byte("betting")},
				{[]byte("choice[choice][name]"), []byte("betting_live")},
				{[]byte("choice[choice][choice][name]"), []byte("betting_live_null")},
				{[]byte("choice[choice][choice][choice][name]"), []byte("betting_live_null_" + strconv.Itoa(i))},
				{[]byte("choice[choice][choice][choice][choice][name]"), []byte("betting_live_null_" + strconv.Itoa(i) + "_" + strconv.Itoa(i))},
				{[]byte("choice[choice][choice][choice][choice][choice][name]"), []byte("betting_live_null_" + strconv.Itoa(i) + "_" + strconv.Itoa(i) + "_" + strconv.Itoa(i))},
				{[]byte("choice[choice][choice][choice][choice][choice][choice]"), []byte("null")},
			},
			[][2][]byte{
				{[]byte("Host"), []byte("0.0.0.0:8020")},
				{[]byte("Accept-Encoding"), []byte("gzip, deflate, br")},
				{[]byte("Accept-Language"), []byte("en-US,en;q=0.9")},
				{[]byte("Content-Type"), []byte("application/json")},
			},
		)
		list = append(list, req)
		i++
		//}
		//}
	}
}

// GenerateRandomResponses generates a list of *model.Response, each linked to a random request and containing
// random body data. Used for stress/load/benchmark testing of cache systems.
func GenerateRandomResponses(cfg *config.Cache, path []byte, num int) []*model.Response {
	headers := http.Header{}
	headers.Add("Content-Type", "application/json")
	headers.Add("Vary", "Accept-Encoding, Accept-Language")

	list := make([]*model.Response, 0, num)
	for _, req := range GenerateRandomRequests(cfg, path, num) {
		data := model.NewData(req.Rule(), http.StatusOK, headers, []byte(GenerateRandomString()))
		resp, err := model.NewResponse(
			data, req, cfg,
			func(ctx context.Context) (*model.Data, error) {
				// Dummy revalidator; always returns the same data.
				return data, nil
			},
		)
		if err != nil {
			panic(err)
		}
		list = append(list, resp)
	}
	return list
}

// GenerateRandomString returns a random ASCII string of length between minStrLen and maxStrLen.
func GenerateRandomString() string {
	return `{
		"data": {
			"type": "seo/pagedata",
			"attributes": {
				"title": "1xBet",
				"description": "",
				"metaRobots": [],
				"hierarchyMetaRobots": [
					{
						"name": "robots",
						"content": "noindex, nofollow"
					}
				],
				"ampPageUrl": null,
				"alternativeLinks": [],
				"alternateMedia": [],
				"customCanonical": null,
				"metas": []
			}
		}
	}`
}
