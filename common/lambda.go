package common

import (
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/service/kms"
	"encoding/json"
	"time"
	"github.com/pkg/errors"
	"github.com/eawsy/aws-lambda-go-core/service/lambda/runtime"
	"os"
	"strconv"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/aws"
	"encoding/base64"
)

type UserCertReqJson struct {
	EventType string
	Token Token
	InstanceId string
	PublicKey string
}

type CaKeyBytesProvider interface {
	CaKeyBytes() []byte
}

type PstoreKeyBytesProvider struct {

}

type UserCertRespJson struct {
	SignedPublicKey string
	Expiry int64
}

type LambdaConfig struct {
	KeyId string
	KmsTokenIdentity string
	CaKeyBytes []byte
	ValidityDuration int64
}

func getCaKeyBytes() ([]byte, error) {
	var caKeyBytes []byte

	if pstoreName, found := os.LookupEnv("PSTORE_CA_KEY_BYTES"); found {
		ssmClient := ssm.New(session.New())
		ssmInput := &ssm.GetParametersInput{
			Names: aws.StringSlice([]string{pstoreName}),
			WithDecryption: aws.Bool(true),
		}

		ssmResp, err := ssmClient.GetParameters(ssmInput)
		if err != nil {
			return nil, errors.Wrap(err, "decrypting key bytes from pstore")
		}

		valstr := ssmResp.Parameters[0].Value
		caKeyBytes = []byte(*valstr)
	} else if kmsEncrypted, found := os.LookupEnv("KMS_B64_CA_KEY_BYTES"); found {
		kmsClient := kms.New(session.New())

		b64dec, err := base64.StdEncoding.DecodeString(kmsEncrypted)
		if err != nil {
			return nil, errors.Wrap(err, "base64 decoding kms-encrypted ca key bytes")
		}

		kmsInput := &kms.DecryptInput{CiphertextBlob: b64dec}
		kmsResp, err := kmsClient.Decrypt(kmsInput)
		if err != nil {
			return nil, errors.Wrap(err, "decrypting kms-encrypted ca key bytes")
		}

		caKeyBytes = kmsResp.Plaintext
	} else if raw, found := os.LookupEnv("CA_KEY_BYTES"); found {
		caKeyBytes = []byte(raw)
	} else {
		return nil, errors.New("no ca key bytes provided")
	}

	return caKeyBytes, nil
}

func LambdaHandle(evt json.RawMessage, ctx *runtime.Context) (interface{}, error) {
	req := UserCertReqJson{}
	err := json.Unmarshal(evt, &req)
	if err != nil {
		return nil, errors.Wrap(err, "unmarshalling input")
	}

	caKeyBytes, err := getCaKeyBytes()
	if err != nil {
		return nil, err
	}

	validity, err := strconv.ParseInt(os.Getenv("VALIDITY_DURATION"), 10, 64)

	config := LambdaConfig{
		KeyId: os.Getenv("KMS_KEY_ID"),
		KmsTokenIdentity: os.Getenv("KMS_TOKEN_IDENTITY"),
		CaKeyBytes: caKeyBytes,
		ValidityDuration: validity,
	}

	resp, err := DoUserCertReq(req, config)
	return resp, err
}

func DoUserCertReq(req UserCertReqJson, config LambdaConfig) (*UserCertRespJson, error) {
	sessOpts := session.Options{
		SharedConfigState: session.SharedConfigEnable,
		AssumeRoleTokenProvider: stscreds.StdinTokenProvider,
	}

	sess, err := session.NewSessionWithOptions(sessOpts)
	if err != nil {
		return nil, errors.Wrap(err, "creating aws session")
	}

	if !ValidateToken(sess, req.Token, config.KeyId) {
		return nil, errors.New("invalid token")
	}

	signed, err := SignSsh(config.CaKeyBytes, []byte(req.PublicKey), config.ValidityDuration, req.Token.Params.From, []string{})
	if err != nil {
		return nil, errors.Wrap(err, "signing ssh key")
	}

	expiry := time.Now().Add(time.Duration(config.ValidityDuration) * time.Second)

	resp := UserCertRespJson{
		SignedPublicKey: *signed,
		Expiry: expiry.Unix(),
	}

	return &resp, nil
}
