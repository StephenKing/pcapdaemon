package main

import (
  "bytes"
  "fmt"
  "io"
  "log"
  "strconv"
  "strings"
  "mime/multipart"
  "net/textproto"
  "net/http"
  "crypto/tls"

  // amamzon stuff
  "github.com/aws/aws-sdk-go/aws"
  "github.com/aws/aws-sdk-go/aws/awserr"
  "github.com/aws/aws-sdk-go/aws/awsutil"
  "github.com/aws/aws-sdk-go/service/s3"
  "github.com/aws/aws-sdk-go/aws/session"
)


func newBufferUploadRequest(uri string, params map[string]string, paramName string, buf bytes.Buffer, filename string) (*http.Request, error)  {
    var err error
    body := &bytes.Buffer{}
    writer := multipart.NewWriter(body)
    if err != nil {
        return nil, err
    }
    mh := make(textproto.MIMEHeader)
    mh.Set("Content-Type", "application/octet-stream")
    mh.Set("Content-Disposition", "form-data; name=\"file\"; filename=\"" + filename + "\"")
    part, err := writer.CreatePart(mh)
    if nil != err {
        panic(err.Error())
    }
    _, err = io.Copy(part, &buf)

    for key, val := range params {
        _ = writer.WriteField(key, val)
    }

    err = writer.Close()
    if err != nil {
        return nil, err
    }

    myReq, myErr := http.NewRequest("POST", uri, body)
    myReq.Header.Add("Content-Type", "multipart/form-data; boundary=" + writer.Boundary())
    if myErr != nil {
        log.Fatal(myErr)
    }

    return myReq, myErr
}

func postBufferCloudshark(scheme string, host string, port int, token string, buf bytes.Buffer, filename string, tags string)  {

    var url string
    extraParams := map[string]string{
        "additional_tags":        tags,
    }
    if(port != 80 && port != 443)  {
        url = scheme + "://" + host + ":" + strconv.Itoa(port) + "/api/v1/" + token + "/upload"
    } else  {
        url = scheme + "://" + host + "/api/v1/" + token + "/upload"
    }

    request, err := newBufferUploadRequest(url, extraParams, "file", buf, filename)
    if err != nil {
        log.Println(err)
		return
    }

    tr := &http.Transport{
        TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
    }
    client := &http.Client{Transport: tr}
    resp, err := client.Do(request)
    if err != nil {
        log.Println(err)
		return
    } else {
        body := &bytes.Buffer{}
        _, err := body.ReadFrom(resp.Body)
        if err != nil {
            log.Println(err)
        }
        resp.Body.Close()
        fmt.Println(resp.StatusCode)
        fmt.Println(resp.Header)
        fmt.Println(body)
    }
}

func postS3(config aws.Config, bucket string, buf bytes.Buffer, filename string, tags string, folder string, acl string, enc bool)  {
	
	s3sess, err := session.NewSession(&config)
	if err != nil {
		// Handle Session creation error
		log.Println("Error creating S3 session: ", err)
		return
	}

	s3client := s3.New(s3sess)
	fileBytes := bytes.NewReader(buf.Bytes()) // convert to io.ReadSeeker type
	fileType := http.DetectContentType(buf.Bytes())
	path := "/" + folder + "/" + filename

    params := &s3.PutObjectInput{
		Bucket:        aws.String(bucket), // required
		Key:           aws.String(path),       // required
        ACL:           aws.String(acl),
        Body:          fileBytes,
        ContentLength: aws.Int64(int64(len(buf.Bytes()))),
        ContentType:   aws.String(fileType),
        Metadata: map[string]*string{
			"tags": aws.String(tags), //required
        },
	}

	// Set the encryption if true
	if(enc)  {
		params.ServerSideEncryption = aws.String("AES256")
	}

    result, err := s3client.PutObject(params)
    if err != nil {
		if awsErr, ok := err.(awserr.Error); ok {
			// Generic AWS Error with Code, Message, and original error (if any)
            fmt.Println("S3 Error: ", awsErr.Code(), awsErr.Message(), awsErr.OrigErr())
            log.Println("S3 Error: ", awsErr.Code(), awsErr.Message(), awsErr.OrigErr())
            if reqErr, ok := err.(awserr.RequestFailure); ok {
				// A service error occurred
                fmt.Println("S3 Error: ", reqErr.Code(), reqErr.Message(), reqErr.StatusCode(), reqErr.RequestID())
                log.Println("S3 Error: ", reqErr.Code(), reqErr.Message(), reqErr.StatusCode(), reqErr.RequestID())
            }
		} else {
			// This case should never be hit, the SDK should always return an
            // error which satisfies the awserr.Error interface.
            log.Println("S3 Error: ", err.Error())
        }
	} else {
		log.Println("S3 Upload successful: ", filename, " ", strings.TrimSpace(awsutil.StringValue(result)))
		fmt.Println("S3 Upload successful: ", filename, " ", strings.TrimSpace(awsutil.StringValue(result)))
	}
}

