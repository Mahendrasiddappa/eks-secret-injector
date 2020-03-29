package main

import (
	"encoding/json"
	"fmt"
	"net/http"
        "strings"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/appscode/jsonpatch"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
)

const (
        awsSecretsInject = "aws-secrets-inject"
        awsSecretsInjectStatus = "aws.secrets-inject-status"
        awsSecretsKey = "aws.secrets-key"
        awsSecretsRegion = "aws.secrets-region"
)

var ignoredNamespaces = []string {
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

func serverError(err error) (events.APIGatewayProxyResponse, error) {
	fmt.Println(err.Error())
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusInternalServerError,
		Headers: map[string]string{
			"Access-Control-Allow-Origin": "*",
		},
		Body: fmt.Sprintf("%v", err.Error()),
	}, nil
}

func responseAdmissionReview(review *admissionv1beta1.AdmissionReview) (events.APIGatewayProxyResponse, error) {
	reviewjson, err := json.Marshal(review)
	if err != nil {
		return serverError(fmt.Errorf("Unexpected decoding error: %v", err))
	}
	fmt.Println(string(reviewjson))
	return events.APIGatewayProxyResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Access-Control-Allow-Origin": "*",
			"Content-Type":                "application/json",
		},
		Body: string(reviewjson),
	}, nil
}

func genCodec() serializer.CodecFactory {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &v1.Secret{})
	scheme.AddKnownTypes(schema.GroupVersion{Group: "", Version: "v1"}, &v1.Pod{})
        codecs := serializer.NewCodecFactory(scheme)
	_ = runtime.ObjectDefaulter(scheme)
	// fmt.Printf("DEBUG:: SCHEME\n %v\n", scheme)
	return codecs
}

func createSecret(namespace, name, payload string) (string, error) {
	svc := secretsmanager.New(session.New())
	input := &secretsmanager.CreateSecretInput{
		Description:  aws.String("A native secret managed by the NaSe Webhook"),
		Name:         aws.String(fmt.Sprintf("%v.%v", namespace, name)),
		SecretString: aws.String(payload),
	}
	result, err := svc.CreateSecret(input)
	if err != nil {
		return "", err
	}
	return *result.ARN, nil
}



func mutationRequired(ignoredList []string, metadata *metav1.ObjectMeta) bool {
	// skip special kubernete system namespaces
	for _, namespace := range ignoredList {
		if metadata.Namespace == namespace {
			fmt.Println("Skip mutation for %v for it' in special namespace:%v", metadata.Name, metadata.Namespace)
			return false
		}
	}

	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
        fmt.Println("req 1")
	status := annotations[awsSecretsInjectStatus]
	fmt.Println("req 2")
	// determine whether to perform mutation based on annotation for the target resource
	var required bool
	if strings.ToLower(status) == "injected" {
		required = false;
                fmt.Println("req 3")
	} else {
                fmt.Println("req 4")
		switch strings.ToLower(annotations[awsSecretsInject]) {
		default:
                        fmt.Println("req 5")
			required = false
		case "y", "yes", "true", "on":
			required = true
		}
	}
	fmt.Println("req 6")
	fmt.Println("Mutation policy for %v/%v: status: %q required:%v", metadata.Namespace, metadata.Name, status, required)

	return required
}


func mutate(body string) (events.APIGatewayProxyResponse, error) {
        codecs := genCodec()
	reviewGVK := admissionv1beta1.SchemeGroupVersion.WithKind("AdmissionReview")
	obj, gvk, err := codecs.UniversalDeserializer().Decode([]byte(body), &reviewGVK, &admissionv1beta1.AdmissionReview{})
	if err != nil {
		return serverError(fmt.Errorf("Can't decode body: %v", err))
	}
	review, ok := obj.(*admissionv1beta1.AdmissionReview)
	if !ok {
		serverError(fmt.Errorf("Unexpected GroupVersionKind: %s", gvk))
	}
	if review.Request == nil {
		return serverError(fmt.Errorf("Unexpected nil request"))
	}
	review.Response = &admissionv1beta1.AdmissionResponse{
		UID: review.Request.UID,
	}
	if review.Request.Object.Object == nil {
		var err error
		review.Request.Object.Object, _, err = codecs.UniversalDeserializer().Decode(review.Request.Object.Raw, nil, nil)
		if err != nil {
			review.Response.Result = &metav1.Status{
				Message: err.Error(),
				Status:  metav1.StatusFailure,
			}
			return responseAdmissionReview(review)
		}
	}
	original := review.Request.Object.Raw
	//ns := review.Request.Namespace
	var bs []byte
	switch secret := review.Request.Object.Object.(type) {
        case *v1.Pod:
               fmt.Println("DEBUG:: POD\n%v\n", secret.ObjectMeta)
	       // determine whether to perform mutation
	       if !mutationRequired(ignoredNamespaces, &secret.ObjectMeta) {
	         	fmt.Println("Skipping mutation for %s/%s due to policy check", secret.Namespace, secret.Name)
	        	//return admissionv1beta1.AdmissionResponse {
		        //	Allowed: true, 
		        //}
                         review.Response.Result = &metav1.Status{
                                 Message: "Skipping mutation",
                                 Status:  metav1.StatusFailure,
                         }
                        return responseAdmissionReview(review)
	       }
               var initref = []v1.Container{
               v1.Container {
                 Name: "aws-secrets",
                 Image: "jicowan/jicowan_aws-secrets-manager:v0.3",
               },
               }
              
               secret.Spec.InitContainers = initref 
               fmt.Println("DEBUG:: POD\n%v\n", secret.Spec.InitContainers)


               // Adding volume
               var initVolumes = []v1.Volume{
               v1.Volume{
                  VolumeSource: v1.VolumeSource{
                  EmptyDir: new(v1.EmptyDirVolumeSource),
                  },
                  Name: "secretmanager-secret",
                 },
                }

               // Add mount Path
               var initVolumeMount = v1.VolumeMount{Name: "secretmanager-secret", MountPath:"/tmp"}
               initref[0].VolumeMounts = append(initref[0].VolumeMounts, initVolumeMount)

               //Add Environment Vars
<<<<<<< HEAD
               annotations := secret.ObjectMeta.GetAnnotations()
               fmt.Println("printing annotations:", annotations)
               var initEnv1, initEnv2 v1.EnvVar
               if annotations[awsSecretsRegion] != "" {
                 initEnv1 = v1.EnvVar{Name: "AWS_REGION", Value: annotations[awsSecretsRegion]}
                 initref[0].Env = append(initref[0].Env, initEnv1)
               }
               if annotations[awsSecretsKey] != "" {
                 initEnv2 = v1.EnvVar{Name: "SECRET_NAME", Value: annotations[awsSecretsKey]} 
                 initref[0].Env = append(initref[0].Env, initEnv2)
               }
=======
               var initEnv1 = v1.EnvVar{Name: "AWS_REGION", Value: "us-east-1"}
               var initEnv2 = v1.EnvVar{Name: "SECRET_NAME", Value: "PodSecret"} 
               initref[0].Env = append(initref[0].Env, initEnv1)
               initref[0].Env = append(initref[0].Env, initEnv2)
>>>>>>> 435e6404e861760bb1ce5f7f3e9fa665ec33aec0
 
               //Changing the Pod
               secret.Spec.Volumes = append(secret.Spec.Volumes, initVolumes[0])
               secret.Spec.Containers[0].VolumeMounts = append(secret.Spec.Containers[0].VolumeMounts, initVolumeMount)
               secret.Spec.InitContainers = initref 
                
               


               bs, err = json.Marshal(secret)
	default:  
               fmt.Println("Entered Default Switch")
		review.Response.Result = &metav1.Status{
			Message: fmt.Sprintf("Unexpected type %T", review.Request.Object.Object),
			Status:  metav1.StatusFailure,
		}
		return responseAdmissionReview(review)
	}
	ops, err := jsonpatch.CreatePatch(original, bs)

        fmt.Println("Test Output")	
        fmt.Println(ops)
        fmt.Println("Done Printing ops")
        if err != nil {
		return serverError(fmt.Errorf("Unexpected diff error: %v", err))
	}
	review.Response.Patch, err = json.Marshal(ops)
	if err != nil {
		return serverError(fmt.Errorf("Unexpected patch encoding error: %v", err))
	}
	typ := admissionv1beta1.PatchTypeJSONPatch
	review.Response.PatchType = &typ
	review.Response.Allowed = true
	return responseAdmissionReview(review)
}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	fmt.Printf("DEBUG:: native secrets webhook start\n")
        fmt.Println(request)
        fmt.Println(request.Body)
	response, err := mutate(request.Body)
	if err != nil {
		return serverError(err)
	}
	fmt.Printf("DEBUG:: native secrets webhook done\n")
	return response, nil
}

func main() {
	lambda.Start(handler)
}
