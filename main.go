package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

// #region main (testing)

func main() {
	GoogleCredentials, err := GetClientCredentialsFromOAuthJson()
	if err != nil {
		fmt.Println("Error getting client credentials:", err)
		return
	}
	accessToken, err := GoogleCredentials.GetAccessToken("franlegon.backup1@gmail.com")
	if err != nil {
		fmt.Println("Error getting access token:", err)
		return
	}

	sharedAlbums, err := GetSharedAlbums(accessToken.AccessToken)
	if err != nil {
		fmt.Println("Error getting shared albums:", err)
		return
	}
	fmt.Println("Shared Albums:")
	for _, album := range sharedAlbums {
		fmt.Println(album)
	}

}

func main2() {

	ClientCredentials, err := GetClientCredentialsFromOAuthJson()
	if err != nil {
		fmt.Println("Error getting client credentials:", err)
		return
	}

	////
	fmt.Println("Client ID:", ClientCredentials.ClientID)

	accessToken, err := ClientCredentials.GetAccessToken("franlegon.backup1@gmail.com")
	if err != nil {
		fmt.Println("Error getting access token:", err)
		return
	}

	//files, err := ListFiles(accessToken.AccessToken)
	//if err != nil {
	//	fmt.Println("Error listing files:", err)
	//	return
	//}
	//fmt.Println("Files:")
	//for _, f := range files.Files {
	//	fmt.Println(f)
	//}
	fmt.Println("--------------------------------------------------------------------")
	quota, err := GetStorageQuota(accessToken.AccessToken)
	if err != nil {
		fmt.Println("Error getting quota:", err)
		return
	}
	fmt.Println("Quota:/n", quota)
	fmt.Println("--------------------------------------------------------------------")
	fmt.Println("Quota in GigaBytes:/n", quota.SeeInGigaBytes())
	fmt.Println("--------------------------------------------------------------------")
}

func main3() {
	WhatsappCredentials, err := GetWhatsappCredentials()
	if err != nil {
		fmt.Println("Error getting whatsapp credentials:", err)
		return
	}
	err = SendWhatsappMessage(WhatsappCredentials.AccessToken, "Hello from Go!", WhatsappCredentials.To)
	if err != nil {
		fmt.Println("Error sending message:", err)
		return
	}
}

// #endregion main (testing)

// #region Encryption
type EncryptionKey struct {
	KeyBase64 string `json:"keyBase64"`
	IvBase64  string `json:"ivBase64"`
}

func DecryptFile(filename string) error {
	// Read the encryption key JSON file
	keyData, err := os.ReadFile("Credentials_EncriptionKey.json")
	if err != nil {
		return err
	}

	// Unmarshal the JSON data into the EncryptionKey struct
	var encKey EncryptionKey
	err = json.Unmarshal(keyData, &encKey)
	if err != nil {
		return err
	}

	// Decode the base64 encoded key and IV
	key, err := base64.StdEncoding.DecodeString(encKey.KeyBase64)
	if err != nil {
		return err
	}
	iv, err := base64.StdEncoding.DecodeString(encKey.IvBase64)
	if err != nil {
		return err
	}

	// Decryption process
	ciphertext, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	if len(ciphertext) < aes.BlockSize {
		return fmt.Errorf("ciphertext too short")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(ciphertext, ciphertext)

	// remove padding
	padding := ciphertext[len(ciphertext)-1] // Get the last byte, which indicates padding length
	padLen := int(padding)                   // Convert to int for slicing

	if padLen > aes.BlockSize || padLen == 0 {
		return fmt.Errorf("invalid padding")
	}
	for _, padByte := range ciphertext[len(ciphertext)-padLen:] {
		if padByte != padding {
			return fmt.Errorf("invalid padding byte")
		}
	}
	ciphertext = ciphertext[:len(ciphertext)-padLen]

	// Write the decrypted file
	decryptedFilename := strings.TrimSuffix(filename, ".enc")
	err = os.WriteFile(decryptedFilename, ciphertext, 0644)
	if err != nil {
		return err
	}

	return nil
}

// #endregion Encryption

// #region Google

type AccessToken struct {
	AccessToken string `json:"access_token"`
	Duration    int    `json:"expires_in"`
	RetrievedAt time.Time
}

func (t *AccessToken) ExpirationTime() time.Time {
	return t.RetrievedAt.Add(time.Duration(t.Duration) * time.Second)
}
func (t *AccessToken) IsExpired() bool {
	return time.Now().After(t.ExpirationTime())
}
func (t *AccessToken) ExpiresIn() time.Duration {
	return time.Until(t.ExpirationTime())
}

type WebOAuthClientJson struct {
	Web ClientCredentials `json:"web"`
}
type ClientCredentials struct {
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	TokenUri      string `json:"token_uri"`
	RefreshTokens map[string]string
	AccessTokens  map[string]AccessToken
}

func GetClientCredentialsFromOAuthJson() (ClientCredentials, error) {

	err := DecryptFile("Credentials_OAuthClient.json.enc")
	if err != nil {
		fmt.Println("Error decrypting file:", err)
		return ClientCredentials{}, err
	}
	defer os.Remove("Credentials_OAuthClient.json")

	var c ClientCredentials
	file, err := os.Open("Credentials_OAuthClient.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return c, err
	}
	defer file.Close()

	var webOAuthClientJson WebOAuthClientJson
	if err := json.NewDecoder(file).Decode(&webOAuthClientJson); err != nil {
		fmt.Println("Error decoding JSON:", err)
		return c, err
	}

	webOAuthClientJson.Web.AccessTokens = make(map[string]AccessToken)
	_, err = webOAuthClientJson.Web.GetRefreshTokensMap()
	if err != nil {
		fmt.Println("Error getting refresh tokens map:", err)
		return webOAuthClientJson.Web, err
	}

	return webOAuthClientJson.Web, nil
}

func (c *ClientCredentials) GetAccessToken(user string) (AccessToken, error) {
	if c.AccessTokens[user].AccessToken != "" {
		return c.AccessTokens[user], nil
	}
	if c.RefreshTokens[user] == "" {
		return c.AccessTokens[user], fmt.Errorf("refreshToken is empty for user %s", user)
	}

	data := map[string]string{
		"refresh_token": c.RefreshTokens[user],
		"client_id":     c.ClientID,
		"client_secret": c.ClientSecret,
		"grant_type":    "refresh_token",
	}
	jsonData, _ := json.Marshal(data)

	resp, err := http.Post(c.TokenUri, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return c.AccessTokens[user], err
	}

	var tokenResp AccessToken
	json.NewDecoder(resp.Body).Decode(&tokenResp)

	tokenResp.RetrievedAt = time.Now()
	c.AccessTokens[user] = tokenResp

	return c.AccessTokens[user], nil
}

func (c *ClientCredentials) GetRefreshTokensMap() (map[string]string, error) {

	err := DecryptFile("Credentials_UsersRefreshTokens.json.enc")
	if err != nil {
		return nil, err
	}
	defer os.Remove("Credentials_UsersRefreshTokens.json")

	file, err := os.Open("Credentials_UsersRefreshTokens.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var refreshTokensMap map[string]string
	if err := json.NewDecoder(file).Decode(&refreshTokensMap); err != nil {
		return nil, err
	}

	c.RefreshTokens = refreshTokensMap

	return c.RefreshTokens, nil
}

// #region Drive

type Files struct {
	Files         []File `json:"files"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}
type File struct {
	Name     string `json:"name"`
	Id       string `json:"id"`
	Kind     string `json:"kind"`
	MimeType string `json:"mimeType"`
	Size     string `json:"size"`
	Owners   []struct {
		DisplayName  string `json:"displayName"`
		EmailAddress string `json:"emailAddress"`
	} `json:"owners"`
}

func ListFiles(accessToken string) (Files, error) {
	var allFiles Files
	url := "https://www.googleapis.com/drive/v3/files" + "?fields=nextPageToken,files(id,name,kind,mimeType,owners,size)"
	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return allFiles, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		// Add query parameters if nextPageToken exists
		q := req.URL.Query()
		if allFiles.NextPageToken != "" {
			q.Add("pageToken", allFiles.NextPageToken)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return allFiles, err
		}
		defer resp.Body.Close()

		var pageFiles Files
		if err := json.NewDecoder(resp.Body).Decode(&pageFiles); err != nil {
			return allFiles, err
		}

		// Append the files from the current page to the allFiles
		allFiles.Files = append(allFiles.Files, pageFiles.Files...)

		// Break the loop if there is no nextPageToken
		if pageFiles.NextPageToken == "" {
			break
		} else {
			allFiles.NextPageToken = pageFiles.NextPageToken
		}
	}

	return allFiles, nil
}

func TransferOwnership(fileID string, accessToken string, newOwnerEmail string) error {
	url := "https://www.googleapis.com/drive/v3/files/" + fileID + "/permissions"
	req, err := http.NewRequest("POST", url, strings.NewReader(`{
		"role": "owner",
		"type": "user",
		"emailAddress": "`+newOwnerEmail+`",
		"TransferOwnership": true
	}`))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("transfer ownership failed: %s", resp.Status)
	}

	return nil
}

type StorageQuota struct {
	Limit             int64
	UsageInDrive      int64
	Usage             int64
	UsageInDriveTrash int64
	Free              int64
}

func GetStorageQuota(accessToken string) (StorageQuota, error) {
	var quota StorageQuota
	url := "https://www.googleapis.com/drive/v3/about?fields=storageQuota"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return quota, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return quota, err
	}
	defer resp.Body.Close()
	//////VMT
	//bodyBytes, _ := io.ReadAll(resp.Body)
	//fmt.Println(string(bodyBytes))
	//////VMT
	var quotaAsStrings struct {
		StorageQuota struct {
			Limit             string `json:"limit"`
			UsageInDrive      string `json:"usageInDrive"`
			Usage             string `json:"usage"`
			UsageInDriveTrash string `json:"usageInDriveTrash"`
		} `json:"storageQuota"`
	}
	err = json.NewDecoder(resp.Body).Decode(&quotaAsStrings)
	if err != nil {
		return quota, err
	}
	quota.Limit, err = strconv.ParseInt(quotaAsStrings.StorageQuota.Limit, 10, 64)
	if err != nil {
		return quota, err
	}
	quota.UsageInDrive, err = strconv.ParseInt(quotaAsStrings.StorageQuota.UsageInDrive, 10, 64)
	if err != nil {
		return quota, err
	}
	quota.Usage, err = strconv.ParseInt(quotaAsStrings.StorageQuota.Usage, 10, 64)
	if err != nil {
		return quota, err
	}
	quota.UsageInDriveTrash, err = strconv.ParseInt(quotaAsStrings.StorageQuota.UsageInDriveTrash, 10, 64)
	if err != nil {
		return quota, err
	}
	quota.Free = quota.Limit - quota.Usage

	return quota, nil
}

func (quota StorageQuota) SeeInGigaBytes() string {
	return fmt.Sprintf("Limit: %.2f GB, UsageInDrive: %.2f GB, Usage: %.2f GB, UsageInDriveTrash: %.2f GB, Free: %.2f GB",
		float64(quota.Limit)/(1<<30), float64(quota.UsageInDrive)/(1<<30), float64(quota.Usage)/(1<<30), float64(quota.UsageInDriveTrash)/(1<<30), float64(quota.Free)/(1<<30))
}

// #endregion Drive

// #region Photos
type MediaItem struct {
	Id            string `json:"id"`
	Description   string `json:"description"`
	ProductUrl    string `json:"productUrl"`
	BaseUrl       string `json:"baseUrl"`
	MimeType      string `json:"mimeType"`
	Filename      string `json:"filename"`
	FileSize      int64
	MediaMetadata struct {
		CreationTime string `json:"creationTime"`
		Width        string `json:"width"`
		Height       string `json:"height"`
	} `json:"mediaMetadata"`
}
type MediaItems struct {
	MediaItems    []MediaItem `json:"mediaItems"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

func ListMediaItems(accessToken string) (MediaItems, error) {
	var allMediaItems MediaItems
	url := "https://photoslibrary.googleapis.com/v1/mediaItems" + "?fields=nextPageToken,mediaItems(id,description,productUrl,baseUrl,mimeType,filename,mediaMetadata)"
	for {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return allMediaItems, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		// Add query parameters if nextPageToken exists
		q := req.URL.Query()
		if allMediaItems.NextPageToken != "" {
			q.Add("pageToken", allMediaItems.NextPageToken)
		}
		req.URL.RawQuery = q.Encode()

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return allMediaItems, err
		}
		defer resp.Body.Close()

		////VMT
		//bodyBytes, _ := io.ReadAll(resp.Body)
		//fmt.Println(string(bodyBytes))
		////VMT

		var pageMediaItems MediaItems
		if err := json.NewDecoder(resp.Body).Decode(&pageMediaItems); err != nil {
			return allMediaItems, err
		}

		// Append the files from the current page to the allFiles
		allMediaItems.MediaItems = append(allMediaItems.MediaItems, pageMediaItems.MediaItems...)

		// Break the loop if there is no nextPageToken
		if pageMediaItems.NextPageToken == "" {
			break
		} else {
			allMediaItems.NextPageToken = pageMediaItems.NextPageToken
			//VMT for testing break
		}
	}

	return allMediaItems, nil
}

func (m *MediaItem) GetFileSize(accessToken string) (int64, error) {
	baseUrl := m.BaseUrl + "=d"
	req, err := http.NewRequest("HEAD", baseUrl, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Add("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Check if Content-Length header is present
	contentLength := resp.Header.Get("Content-Length")
	if contentLength == "" {
		return 0, errors.New("Content-Length header is missing")
	}

	// Convert Content-Length to int64
	size, err := strconv.ParseInt(contentLength, 10, 64)
	if err != nil {
		return 0, err
	}

	// Update the FileSize field in the original MediaItem struct
	m.FileSize = size
	return size, nil
}

func (m MediaItem) StreamDownload(accessToken string) (io.ReadCloser, error) {
	baseUrl := m.BaseUrl + "=d"
	req, err := http.NewRequest("GET", baseUrl, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	// The caller is responsible for closing the response body
	return resp.Body, nil
}

func (m MediaItem) Download(accessToken string, filepath string, filename string) error {
	ioReader, err := m.StreamDownload(accessToken)
	if err != nil {
		return err
	} else if ioReader == nil {
		return errors.New("failed to stream download")
	}
	defer ioReader.Close()

	file, err := os.Create(filepath + filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, ioReader)
	if err != nil {
		return err
	}

	return nil
}

type SharedAlbum struct {
	Id              string `json:"id"`
	Title           string `json:"title"`
	MediaItemsCount int    `json:"mediaItemsCount"`
	ProductUrl      string `json:"productUrl"`
}

func GetSharedAlbums(accessToken string) ([]SharedAlbum, error) {
	var sharedAlbums []SharedAlbum
	url := "https://photoslibrary.googleapis.com/v1/sharedAlbums"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return sharedAlbums, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return sharedAlbums, err
	}
	defer resp.Body.Close()

	var albums struct {
		SharedAlbums []SharedAlbum `json:"sharedAlbums"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&albums); err != nil {
		return sharedAlbums, err
	}

	return albums.SharedAlbums, nil
}

// #endregion Photos

// #endregion Google

// #region Whatsapp
type WhatsappCredentials struct {
	AccessToken string `json:"admin-system-user-access-token"`
	To          int    `json:"to-whatsapp-number"`
}

func GetWhatsappCredentials() (WhatsappCredentials, error) {

	var WhatsappCredentials WhatsappCredentials

	if err := DecryptFile("Credentials_Whatsapp.json.enc"); err != nil {
		fmt.Println("Error decrypting file:", err)
		return WhatsappCredentials, err
	}
	defer os.Remove("Credentials_Whatsapp.json")

	file, err := os.Open("Credentials_Whatsapp.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return WhatsappCredentials, err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&WhatsappCredentials); err != nil {
		fmt.Println("Error decoding JSON:", err)
		return WhatsappCredentials, err
	}

	return WhatsappCredentials, nil
}

func SendWhatsappMessage(accessToken string, message string, to int) error {
	url := "https://graph.facebook.com/v19.0/387981631054756/messages"
	body := fmt.Sprintf(`{
		"messaging_product": "whatsapp",
		"to": "%d",
		"type": "text",
		"text": {
			"preview_url": false,
			"body": "%s"
		}
	}`, to, message)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}

	req.Header.Add("Authorization", "Bearer "+accessToken)
	req.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %v", err)
		}
		bodyMessage := string(bodyBytes)
		return fmt.Errorf("failed to send WhatsApp message, status code: %d, message: %s", resp.StatusCode, bodyMessage)
	}

	return nil
}

// #endregion Whatsapp

// #region ngrok
type NgrokCredentials struct {
	AuthToken      string `json:"authToken"`
	WithForwardsTo string `json:"withForwardsTo"`
	WithDomain     string `json:"withDomain"`
}

func GetNgrokCredentials() (NgrokCredentials, error) {

	var c NgrokCredentials

	if err := DecryptFile("Credentials_Ngrok.json.enc"); err != nil {
		fmt.Println("Error decrypting file:", err)
		return NgrokCredentials{}, err
	}
	defer os.Remove("Credentials_Ngrok.json")

	file, err := os.Open("Credentials_Ngrok.json")
	if err != nil {
		fmt.Println("Error opening file:", err)
		return c, err
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&c); err != nil {
		fmt.Println("Error decoding JSON:", err)
		return c, err
	}

	return c, nil
}

func NgrokHandler() {
	ngrokCredentials, err := GetNgrokCredentials()
	if err != nil {
		fmt.Println("Error getting ngrok credentials:", err)
		return
	}
	ngrokAuth := ngrok.WithAuthtoken(ngrokCredentials.AuthToken)
	tunnel := config.HTTPEndpoint(
		config.WithForwardsTo(ngrokCredentials.WithForwardsTo),
		config.WithDomain(ngrokCredentials.WithDomain),
		config.WithScheme(config.SchemeHTTPS),
		/*
			config.WithAllowCIDRString("0.0.0.0/0"),
			config.WithAllowUserAgent("Mozilla/5.0.*"),
			// config.WithBasicAuth("ngrok", "online1line"),
			config.WithCircuitBreaker(0.5),
			config.WithCompression(),
			config.WithDenyCIDRString("10.1.1.1/32"),
			config.WithDenyUserAgent("EvilCorp.*"),
			// config.WithDomain("<somedomain>.ngrok.io"),
			config.WithMetadata("example secure connection metadata from golang"),
			// config.WithMutualTLSCA(<cert>),
			// config.WithOAuth("google",
			// 	config.WithAllowOAuthEmail("<user>@<domain>"),
			// 	config.WithAllowOAuthDomain("<domain>"),
			// 	config.WithOAuthScope("<scope>"),
			// ),
			// config.WithOIDC("<url>", "<id>", "<secret>",
			// 	config.WithAllowOIDCEmail("<user>@<domain>"),
			// 	config.WithAllowOIDCDomain("<domain>"),
			// 	config.WithOIDCScope("<scope>"),
			// ),
			config.WithProxyProto(config.ProxyProtoNone),
			config.WithRemoveRequestHeader("X-Req-Nope"),
			config.WithRemoveResponseHeader("X-Res-Nope"),
			config.WithRequestHeader("X-Req-Yup", "true"),
			config.WithResponseHeader("X-Res-Yup", "true"),
			config.WithScheme(config.SchemeHTTPS),
			// config.WithWebsocketTCPConversion(),
			// config.WithWebhookVerification("twilio", "asdf"),
		*/
	)
	ln, err := ngrok.Listen(context.Background(), tunnel, ngrokAuth)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Ingress established at:", ln.URL())
	http.HandleFunc("/", Handler)
	log.Fatal(http.Serve(ln, nil))
}

func Handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello from ngrok-go!"))
}

// #endregion ngrok
