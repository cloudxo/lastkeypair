package lastkeypair

import (
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws"
	"time"
	"encoding/json"
	"log"
	"encoding/base64"
	"github.com/aws/aws-sdk-go/service/sts"
	"strings"
	"golang.org/x/crypto/ssh"
	"fmt"
	"github.com/pkg/errors"
	"crypto/rand"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/glassechidna/awscredcache"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/pquerna/otp/totp"
	"os"
	"github.com/glassechidna/lastkeypair/pkg/lastkeypair/cli"
)

var ApplicationVersion string
var ApplicationBuildDate string

var DefaultSshPermissions = ssh.Permissions{
	CriticalOptions: map[string]string{},
	Extensions: map[string]string{
		"permit-X11-forwarding":   "",
		"permit-agent-forwarding": "",
		"permit-port-forwarding":  "",
		"permit-pty":              "",
		"permit-user-rc":          "",
	},
}

func SignSsh(caKeyBytes, sshKeyPassphrase, pubkeyBytes []byte, certType uint32, expiry uint64, permissions ssh.Permissions, keyId string, principals []string) (*string, error) {
	var signer ssh.Signer
	var err error

	if len(sshKeyPassphrase) > 0 {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(caKeyBytes, sshKeyPassphrase)
	} else {
		signer, err = ssh.ParsePrivateKey(caKeyBytes)
	}

	if err != nil {
		return nil, errors.Wrap(err, "err parsing ca priv key")
	}

	userPubkey, _, _, _, err := ssh.ParseAuthorizedKey(pubkeyBytes)
	if err != nil {
		return nil, errors.Wrap(err, "err parsing user pub key")
	}

	now := time.Now()
	after := now.Add(-300 * time.Second)

	cert := &ssh.Certificate{
		//Nonce: is generated by cert.SignCert
		Key: userPubkey,
		Serial: 0,
		CertType: certType,
		KeyId: keyId,
		ValidPrincipals: principals,
		ValidAfter: uint64(after.Unix()),
		ValidBefore: expiry,
		Permissions: permissions,
		Reserved: []byte{},
	}

	randSource := rand.Reader
	err = cert.SignCert(randSource, signer)
	if err != nil {
		return nil, errors.Wrap(err, "err signing cert")
	}

	signed := cert.Marshal()

	b64 := base64.StdEncoding.EncodeToString(signed)
	formatted := fmt.Sprintf("%s %s", cert.Type(), b64)
	return &formatted, nil
}

func ClientAwsSession(profile, region string) *session.Session {
	provider := awscredcache.NewAwsCacheCredProvider(profile)
	provider.MfaCodeProvider = func(mfaSecret string) (string, error) {
		if len(mfaSecret) > 0 {
			return totp.GenerateCode(mfaSecret, time.Now())
		} else {
			return stscreds.StdinTokenProvider()
		}
	}

	creds := credentials.NewCredentials(provider.WrapInChain())

	sessOpts := session.Options{
		SharedConfigState: session.SharedConfigEnable,
		AssumeRoleTokenProvider: stscreds.StdinTokenProvider,
		Config: aws.Config{Credentials: creds},
	}

	if len(os.Getenv("LKP_AWS_VERBOSE")) > 0 {
		sessOpts.Config.LogLevel = aws.LogLevel(aws.LogDebugWithHTTPBody)
	}

	if len(profile) > 0 {
		sessOpts.Profile = profile
	}

	sess, _ := session.NewSessionWithOptions(sessOpts)

	userAgentHandler := request.NamedHandler{
		Name: "LastKeypair.UserAgentHandler",
		Fn:   request.MakeAddToUserAgentHandler("LastKeypair", ApplicationVersion),
	}
	sess.Handlers.Build.PushBackNamed(userAgentHandler)

	if len(region) > 0 {
		sess.Config.Region = aws.String(region)
	}

	return sess
}

type PlaintextPayload struct {
	NotBefore float64 // this is what json.unmarshal wants
	NotAfter float64
}

func kmsClientForKeyId(sess *session.Session, keyId string) *kms.KMS {
	if strings.HasPrefix(keyId, "arn:aws:kms") {
		parts := strings.Split(keyId, ":")
		region := parts[3]
		sess = sess.Copy(aws.NewConfig().WithRegion(region))
	}

	return kms.New(sess)
}

func CreateToken(sess *session.Session, params TokenParams, keyId string) Token {
	context := params.ToKmsContext()

	now := float64(time.Now().Unix())
	end := now + 3600 // 1 hour

	payload := PlaintextPayload{
		NotBefore: now,
		NotAfter: end,
	}

	plaintext, err := json.Marshal(&payload)
	if err != nil {
		log.Panicf("Payload json encoding error: %s", err.Error())
	}

	keyArn, err := cli.FullKmsKey(sess, keyId)
	if err != nil {
		log.Panicf("Determining KMS key ARN from key id/alias: %s", err.Error())
	}

	input := &kms.EncryptInput{
		Plaintext: plaintext,
		KeyId: &keyArn,
		EncryptionContext: context,
	}

	client := kmsClientForKeyId(sess, keyArn)
	response, err := client.Encrypt(input)
	if err != nil {
		log.Panicf("Encryption error: %s", err.Error())
	}

	blob := response.CiphertextBlob
	return Token{Params: params, Signature: blob}
}

func ValidateToken(sess *session.Session, token Token, expectedKeyId string) bool {
	context := token.Params.ToKmsContext()

	input := &kms.DecryptInput{
		CiphertextBlob: token.Signature,
		EncryptionContext: context,
	}

	client := kms.New(sess)
	response, err := client.Decrypt(input)
	if err != nil {
		log.Panicf("Decryption error: %s", err.Error())
	}

	/* We verify that the encryption key used is the one that we expected it to be.
	   This is very important, as an attacker could submit ciphertext encrypted with
	   a key they control that grants our Lambda permission to decrypt. Perhaps it
	   would be worth implementing some kind of alert here?
	 */
	if expectedKeyId != *response.KeyId {
		log.Panicf("Mismatching KMS key ids: %s and %s", expectedKeyId, *response.KeyId)
	}

	payload := PlaintextPayload{}
	err = json.Unmarshal([]byte(response.Plaintext), &payload)
	if err != nil {
		return false
		//return nil, errors.Wrap(err, "decoding token json")
	}

	now := float64(time.Now().Unix())
	if now < payload.NotBefore || now > payload.NotAfter {
		return false
		//return nil, errors.New("expired token")
	}

	return true
}

type StsIdentity struct {
	AccountId string
	UserId string
	Username string
	Type string
}

func CallerIdentityUser(sess *session.Session) (*StsIdentity, error) {
	client := sts.New(sess)
	response, err := client.GetCallerIdentity(&sts.GetCallerIdentityInput{})

	if err == nil {
		arn := *response.Arn
		parts := strings.SplitN(arn, ":", 6)

		if strings.HasPrefix(parts[5], "user/") {
			name := parts[5][5:]
			return &StsIdentity{
				AccountId: *response.Account,
				UserId: *response.UserId,
				Username: name,
				Type: "User",
			}, nil
		} else if strings.HasPrefix(parts[5], "assumed-role/") {
			return &StsIdentity{
				AccountId: *response.Account,
				UserId: *response.UserId,
				Username: "",
				Type: "AssumedRole",
			}, nil
		} else {
			return nil, errors.New("unsupported IAM identity type")
		}
	} else {
		return nil, err
	}
}