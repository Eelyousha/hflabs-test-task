package main

import (
	"context"
	"encoding/json"
	"fmt"
	strip "github.com/grokify/html-strip-tags-go"
	"golang.org/x/net/html"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func getDataFromTag(fragment, openTag, closeTag string) (data string) {
	pos := strings.Index(fragment, openTag)
	endPos := strings.Index(fragment[pos:], closeTag)
	data = fragment[pos : pos+endPos+len(closeTag)]
	return data
}

func getHeaderValues(webPage string) (res []string) {
	for strings.Index(webPage, "<th ") != -1 {
		tagBegIdx := strings.Index(webPage, "<th ")
		tagEndIdx := strings.Index(webPage[tagBegIdx:], ">") + 1 + tagBegIdx
		closeTagIdx := strings.Index(webPage[tagBegIdx:], "</th>") + tagBegIdx
		value := webPage[tagEndIdx:closeTagIdx]
		res = append(res, value)
		webPage = webPage[closeTagIdx:]
	}

	return res
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func parseTable(text string) (res [][]interface{}) {
	tkn := html.NewTokenizer(strings.NewReader(text))
	var isTd bool
	var n int
	var tmpInterface []interface{}
	for {
		tt := tkn.Next()
		switch {
		case tt == html.ErrorToken:
			return res
		case tt == html.StartTagToken:
			t := tkn.Token()
			isTd = t.Data == "td"
		case tt == html.TextToken:
			t := tkn.Token()
			if isTd {
				tmpInterface = append(tmpInterface, t.Data)
				n++
			}
			if isTd && n%2 == 0 {
				res = append(res, tmpInterface)
				tmpInterface = make([]interface{}, 0)
			}
			isTd = false
		}
	}
}

func main() {
	for {
		url := "https://confluence.hflabs.ru/pages/viewpage.action?pageId=1181220999"
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		htmlData, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		data := getDataFromTag(string(htmlData), "<div class=\"table-wrap\">", "</div>")
		headers := getHeaderValues(data)

		ctx := context.Background()
		b, err := os.ReadFile("credentials.json")
		if err != nil {
			log.Fatalf("Unable to read client secret file: %v", err)
		}

		// If modifying these scopes, delete your previously saved token.json.
		config, err := google.ConfigFromJSON(b, "https://www.googleapis.com/auth/spreadsheets")
		if err != nil {
			log.Fatalf("Unable to parse client secret file to config: %v", err)
		}
		client := getClient(config)

		srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
		if err != nil {
			log.Fatalf("Unable to retrieve Sheets client: %v", err)
		}

		spreadsheetId := "1ubILHw8TfZLIMMjvy0POPWsu2UsgGs70dn6EvCocW38"
		headerRange := "Page1!A1:B"

		headerInput := make([][]interface{}, 1)
		headerInput[0] = make([]interface{}, len(headers))
		for i, _ := range headerInput[0] {
			headerInput[0][i] = headers[i]
		}
		hr := &sheets.ValueRange{Values: headerInput}
		_, err = srv.Spreadsheets.Values.Update(spreadsheetId, headerRange, hr).ValueInputOption("RAW").Context(ctx).Do()
		if err != nil {
			log.Fatal(err)
		}

		fieldsRange := "Page1!A2:B" // TODO: Update placeholder value.
		// How the input data should be interpreted.
		valueInputOption := "RAW" // TODO: Update placeholder values
		valueInput := make([][]interface{}, 0)
		for strings.Index(data, "<td ") != -1 {
			var tmpVal []string
			tmpInterface := make([]interface{}, 2)
			for i := 0; i < 2; i++ {
				tagBegIdx := strings.Index(data, "<td ")
				tagEndIdx := strings.Index(data[tagBegIdx:], ">") + 1 + tagBegIdx
				closeTagIdx := strings.Index(data[tagBegIdx:], "</td>") + tagBegIdx
				tmpVal = append(tmpVal, data[tagEndIdx:closeTagIdx])
				data = data[closeTagIdx:]
				tmpVal[i] = strip.StripTags(tmpVal[i])
				tmpInterface[i] = tmpVal[i]
			}
			valueInput = append(valueInput, tmpInterface)
		}

		rb := &sheets.ValueRange{Values: valueInput}

		_, err = srv.Spreadsheets.Values.Update(spreadsheetId, fieldsRange, rb).ValueInputOption(valueInputOption).Context(ctx).Do()
		if err != nil {
			log.Fatal(err)
		}
		time.Sleep(5 * time.Second)
	}
}
