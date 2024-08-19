package controller

import (
	"context"
	"encoding/hex"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/zlabjp/nghttpx-ingress-lb/pkg/nghttpx"
)

var (
	defaultGatewayClassConditions = []metav1.Condition{
		{
			Type:    string(gatewayv1.GatewayClassConditionStatusAccepted),
			Status:  metav1.ConditionUnknown,
			Reason:  string(gatewayv1.GatewayClassReasonPending),
			Message: "Waiting for controller",
		},
	}

	defaultGatewayConditions = []metav1.Condition{
		{
			Type:    string(gatewayv1.GatewayConditionProgrammed),
			Status:  metav1.ConditionUnknown,
			Reason:  string(gatewayv1.GatewayReasonPending),
			Message: "Waiting for controller",
		},
		{
			Type:    string(gatewayv1.GatewayConditionAccepted),
			Status:  metav1.ConditionUnknown,
			Reason:  string(gatewayv1.GatewayReasonPending),
			Message: "Waiting for controller",
		},
	}
)

func TestSyncGatewayClass(t *testing.T) {
	ts := metav1.Now()

	tests := []struct {
		desc           string
		gatewayClass   gatewayv1.GatewayClass
		noUpdate       bool
		wantConditions []metav1.Condition
	}{
		{
			desc: "Accept GatewayClass",
			gatewayClass: gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gc",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: defaultGatewayClassController,
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: defaultGatewayClassConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayClassReasonAccepted),
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "GatewayClass not controlled by this controller",
			gatewayClass: gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gc",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: "other",
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: defaultGatewayClassConditions,
				},
			},
			noUpdate: true,
		},
		{
			desc: "GatewayClass status is up-to-date",
			gatewayClass: gatewayv1.GatewayClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gc",
				},
				Spec: gatewayv1.GatewayClassSpec{
					ControllerName: defaultGatewayClassController,
				},
				Status: gatewayv1.GatewayClassStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayClassConditionStatusAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayClassReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
			noUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true
			f.currentTime = ts

			f.gatewayClassStore = append(f.gatewayClassStore, &tt.gatewayClass)
			f.gatewayObjects = append(f.gatewayObjects, &tt.gatewayClass)

			f.prepare()
			f.setupStore()

			err := f.lc.syncGatewayClass(context.Background(), namespacedName(&tt.gatewayClass))
			if err != nil {
				t.Fatalf("f.lc.syncGatewayClass: %v", err)
			}

			if !tt.noUpdate {
				f.expectUpdateStatusGatewayClassAction(&tt.gatewayClass)
			}

			f.verifyActions()

			if !tt.noUpdate {
				updatedGC, err := f.gatewayClientset.GatewayV1().GatewayClasses().
					Get(context.Background(), tt.gatewayClass.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Unable to get GatewayClass: %v", err)
				}

				if got, want := updatedGC.Status.Conditions, tt.wantConditions; !equality.Semantic.DeepEqual(got, want) {
					t.Errorf("updatedGC.Status.Conditions = %v, want %v", klog.Format(got), klog.Format(want))
				}
			}
		})
	}
}

func TestSyncGateway(t *testing.T) {
	ts := metav1.Now()

	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: defaultGatewayClassController,
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	uncontrolledGC := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "uncontrolled",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "uncontrolled",
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	tests := []struct {
		desc           string
		gateway        gatewayv1.Gateway
		noUpdate       bool
		wantConditions []metav1.Condition
	}{
		{
			desc: "Accept Gateway",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayReasonProgrammed),
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayReasonAccepted),
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway refers to the GatewayClass which is not controlled by this controller",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(uncontrolledGC.Name),
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			noUpdate: true,
		},
		{
			desc: "Gateway status is up-to-date",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.GatewayConditionProgrammed),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayReasonProgrammed),
							LastTransitionTime: ts,
						},
						{
							Type:               string(gatewayv1.GatewayConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.GatewayReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
			noUpdate: true,
		},
		{
			desc: "Gateway has HTTP protocol listener with TLS configuration",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.HTTPProtocolType,
							TLS:      &gatewayv1.GatewayTLSConfig{},
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls must not be set",
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls must not be set",
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway has HTTP protocol listener without TLS configuration",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.HTTPProtocolType,
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayReasonProgrammed),
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayReasonAccepted),
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway has HTTPS protocol listener with TLS configuration and certificate",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.HTTPSProtocolType,
							TLS: &gatewayv1.GatewayTLSConfig{
								Mode: ptr.To(gatewayv1.TLSModeTerminate),
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Name: gatewayv1.ObjectName("cert"),
									},
								},
							},
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayReasonProgrammed),
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionTrue,
					Reason:             string(gatewayv1.GatewayReasonAccepted),
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway has HTTPS protocol listener with TLS configuration but without certificate",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.HTTPSProtocolType,
							TLS:      &gatewayv1.GatewayTLSConfig{},
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls.certificateRefs must contain at least one reference",
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls.certificateRefs must contain at least one reference",
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway has HTTPS protocol listener without TLS configuration",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.HTTPSProtocolType,
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls is required",
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls is required",
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway has HTTPS protocol listener with TLS configuration and TLS mode is not Terminate",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.HTTPSProtocolType,
							TLS: &gatewayv1.GatewayTLSConfig{
								Mode: ptr.To(gatewayv1.TLSModePassthrough),
								CertificateRefs: []gatewayv1.SecretObjectReference{
									{
										Name: gatewayv1.ObjectName("cert"),
									},
								},
							},
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls.mode must be Terminate",
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].tls.mode must be Terminate",
					LastTransitionTime: ts,
				},
			},
		},
		{
			desc: "Gateway has a listener with unsupported protocol",
			gateway: gatewayv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gtw",
					Namespace: "ns",
				},
				Spec: gatewayv1.GatewaySpec{
					GatewayClassName: gatewayv1.ObjectName(gc.Name),
					Listeners: []gatewayv1.Listener{
						{
							Protocol: gatewayv1.TCPProtocolType,
						},
					},
				},
				Status: gatewayv1.GatewayStatus{
					Conditions: defaultGatewayConditions,
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:               string(gatewayv1.GatewayConditionProgrammed),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].protocol is not supported: TCP",
					LastTransitionTime: ts,
				},
				{
					Type:               string(gatewayv1.GatewayConditionAccepted),
					Status:             metav1.ConditionFalse,
					Reason:             string(gatewayv1.GatewayReasonInvalid),
					Message:            ".spec.listeners[0].protocol is not supported: TCP",
					LastTransitionTime: ts,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true
			f.currentTime = ts

			f.gatewayClassStore = append(f.gatewayClassStore, gc, uncontrolledGC)
			f.gatewayStore = append(f.gatewayStore, &tt.gateway)
			f.gatewayObjects = append(f.gatewayObjects, &tt.gateway)

			f.prepare()
			f.setupStore()

			err := f.lc.syncGateway(context.Background(), namespacedName(&tt.gateway))
			if err != nil {
				t.Fatalf("f.lc.syncGateway: %v", err)
			}

			if !tt.noUpdate {
				f.expectUpdateStatusGatewayAction(&tt.gateway)
			}

			f.verifyActions()

			if !tt.noUpdate {
				updatedGtw, err := f.gatewayClientset.GatewayV1().Gateways(tt.gateway.Namespace).
					Get(context.Background(), tt.gateway.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Unable to get Gateway: %v", err)
				}

				if got, want := updatedGtw.Status.Conditions, tt.wantConditions; !equality.Semantic.DeepEqual(got, want) {
					t.Errorf("updatedGtw.Status.Conditions = %v, want %v", klog.Format(got), klog.Format(want))
				}
			}
		})
	}
}

func TestSyncHTTPRoute(t *testing.T) {
	ts := metav1.Now()

	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: defaultGatewayClassController,
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	uncontrolledGC := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "uncontrolled",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "uncontrolled",
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	tests := []struct {
		desc             string
		gateways         []*gatewayv1.Gateway
		httpRoute        gatewayv1.HTTPRoute
		noUpdate         bool
		wantParentStatus []gatewayv1.RouteParentStatus
	}{
		{
			desc: "Accept HTTPRoute",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.RouteReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "HTTPRoute status is up-to-date",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
										Reason: string(gatewayv1.RouteReasonAccepted),
									},
								},
							},
						},
					},
				},
			},
			noUpdate: true,
		},
		{
			desc: "Accept HTTPRoute with SectionName",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name:        "gtw",
						SectionName: ptr.To(gatewayv1.SectionName("http")),
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.RouteReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "Ignore HTTPRoute which is not controlled by this controller",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(uncontrolledGC.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
			},
			noUpdate: true,
		},
		{
			desc: "HTTPRoute which solely refers to Gateway which has not been accepted",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: defaultGatewayConditions,
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionUnknown,
							Reason:             string(gatewayv1.RouteReasonPending),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "HTTPRoute has a ParentRef which refers to non-existing SectionName",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("https")),
							},
						},
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name:        "gtw",
						SectionName: ptr.To(gatewayv1.SectionName("https")),
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.RouteReasonNoMatchingParent),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "Delete ParentStatus if there is no matching listener",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "not-found-gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
										Reason: string(gatewayv1.RouteReasonAccepted),
									},
								},
							},
						},
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.RouteReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "HTTPRoute is acceptable with hostnames",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.org")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
						"www.example.com",
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.RouteReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "HTTPRoute is not acceptable because none of its hostnames are allowed",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.org")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.RouteReasonNoMatchingListenerHostname),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "HTTPRoute is acceptable with hostnames and sectionName",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.org")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
						"www.example.com",
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name:        "gtw",
						SectionName: ptr.To(gatewayv1.SectionName("http")),
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.RouteReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "HTTPRoute is not acceptable with hostnames and sectionName",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.org")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
						"www.example.org",
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name:        "gtw",
						SectionName: ptr.To(gatewayv1.SectionName("http")),
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.RouteReasonNoMatchingListenerHostname),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
		{
			desc: "Multiple Gateways one is OK but the other is not",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw2",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.net")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
							{
								Name: "gtw2",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.com",
					},
				},
			},
			wantParentStatus: []gatewayv1.RouteParentStatus{
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionTrue,
							Reason:             string(gatewayv1.RouteReasonAccepted),
							LastTransitionTime: ts,
						},
					},
				},
				{
					ParentRef: gatewayv1.ParentReference{
						Name: "gtw2",
					},
					ControllerName: defaultGatewayClassController,
					Conditions: []metav1.Condition{
						{
							Type:               string(gatewayv1.RouteConditionAccepted),
							Status:             metav1.ConditionFalse,
							Reason:             string(gatewayv1.RouteReasonNoMatchingListenerHostname),
							LastTransitionTime: ts,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true
			f.currentTime = ts

			f.gatewayClassStore = append(f.gatewayClassStore, gc, uncontrolledGC)
			f.gatewayStore = append(f.gatewayStore, tt.gateways...)
			f.httpRouteStore = append(f.httpRouteStore, &tt.httpRoute)
			f.gatewayObjects = append(f.gatewayObjects, &tt.httpRoute)

			f.prepare()
			f.setupStore()

			err := f.lc.syncHTTPRoute(context.Background(), namespacedName(&tt.httpRoute))
			if err != nil {
				t.Fatalf("f.lc.syncHTTPRoute: %v", err)
			}

			if !tt.noUpdate {
				f.expectUpdateStatusHTTPRouteAction(&tt.httpRoute)
			}

			f.verifyActions()

			if !tt.noUpdate {
				updatedHTTPRoute, err := f.gatewayClientset.GatewayV1().HTTPRoutes(tt.httpRoute.Namespace).
					Get(context.Background(), tt.httpRoute.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("Unable to get HTTPRoute: %v", err)
				}

				if got, want := updatedHTTPRoute.Status.Parents, tt.wantParentStatus; !equality.Semantic.DeepEqual(got, want) {
					t.Errorf("updatedHTTPRoute.Status.Parents = %v, want %v", klog.Format(got), klog.Format(want))
				}
			}
		})
	}
}

func TestCreateGatewayUpstream(t *testing.T) {
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: defaultGatewayClassController,
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	bs1, bes1 := newBackend("alpha", []string{"192.168.10.1"})

	tests := []struct {
		desc       string
		gateways   []*gatewayv1.Gateway
		httpRoutes []*gatewayv1.HTTPRoute
		want       []*nghttpx.Upstream
	}{
		{
			desc: "No HTTPRoutes",
		},
		{
			desc: "Create upstreams",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: metav1.NamespaceDefault,
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName("cert"),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoutes: []*gatewayv1.HTTPRoute{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "auth",
						Namespace: metav1.NamespaceDefault,
					},
					Spec: gatewayv1.HTTPRouteSpec{
						CommonRouteSpec: gatewayv1.CommonRouteSpec{
							ParentRefs: []gatewayv1.ParentReference{
								{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("https")),
								},
							},
						},
						Hostnames: []gatewayv1.Hostname{
							"auth.example.com",
							"www.auth.example.com",
						},
						Rules: []gatewayv1.HTTPRouteRule{
							{
								Matches: []gatewayv1.HTTPRouteMatch{
									{
										Path: &gatewayv1.HTTPPathMatch{
											Value: ptr.To("/login"),
										},
									},
									{
										Path: &gatewayv1.HTTPPathMatch{
											Value: ptr.To("/auth"),
										},
									},
								},
								BackendRefs: []gatewayv1.HTTPBackendRef{
									{
										BackendRef: gatewayv1.BackendRef{
											BackendObjectReference: gatewayv1.BackendObjectReference{
												Name: gatewayv1.ObjectName(bs1.Name),
												Port: ptr.To(gatewayv1.PortNumber(bs1.Spec.Ports[0].Port)),
											},
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.HTTPRouteStatus{
						RouteStatus: gatewayv1.RouteStatus{
							Parents: []gatewayv1.RouteParentStatus{
								{
									ParentRef: gatewayv1.ParentReference{
										Name:        "gtw",
										SectionName: ptr.To(gatewayv1.SectionName("https")),
									},
									ControllerName: defaultGatewayClassController,
									Conditions: []metav1.Condition{
										{
											Type:   string(gatewayv1.RouteConditionAccepted),
											Status: metav1.ConditionTrue,
										},
									},
								},
							},
						},
					},
				},
			},
			want: []*nghttpx.Upstream{
				{
					Name:             "gateway.networking.k8s.io/v1/HTTPRoute:default/alpha,8281;auth.example.com/login",
					GroupVersionKind: gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"),
					Source:           types.NamespacedName{Name: "auth", Namespace: metav1.NamespaceDefault},
					Host:             "auth.example.com",
					Path:             "/login",
					Backends: []nghttpx.Backend{
						{
							Address:  "192.168.10.1",
							Port:     "80",
							Protocol: nghttpx.ProtocolH1,
							Group:    "default/alpha",
						},
					},
					RedirectIfNotTLS:         true,
					Affinity:                 nghttpx.AffinityNone,
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureAuto,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
				{
					Name:             "gateway.networking.k8s.io/v1/HTTPRoute:default/alpha,8281;www.auth.example.com/login",
					GroupVersionKind: gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"),
					Source:           types.NamespacedName{Name: "auth", Namespace: metav1.NamespaceDefault},
					Host:             "www.auth.example.com",
					Path:             "/login",
					Backends: []nghttpx.Backend{
						{
							Address:  "192.168.10.1",
							Port:     "80",
							Protocol: nghttpx.ProtocolH1,
							Group:    "default/alpha",
						},
					},
					RedirectIfNotTLS:         true,
					Affinity:                 nghttpx.AffinityNone,
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureAuto,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
				{
					Name:             "gateway.networking.k8s.io/v1/HTTPRoute:default/alpha,8281;auth.example.com/auth",
					GroupVersionKind: gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"),
					Source:           types.NamespacedName{Name: "auth", Namespace: metav1.NamespaceDefault},
					Host:             "auth.example.com",
					Path:             "/auth",
					Backends: []nghttpx.Backend{
						{
							Address:  "192.168.10.1",
							Port:     "80",
							Protocol: nghttpx.ProtocolH1,
							Group:    "default/alpha",
						},
					},
					RedirectIfNotTLS:         true,
					Affinity:                 nghttpx.AffinityNone,
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureAuto,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
				{
					Name:             "gateway.networking.k8s.io/v1/HTTPRoute:default/alpha,8281;www.auth.example.com/auth",
					GroupVersionKind: gatewayv1.SchemeGroupVersion.WithKind("HTTPRoute"),
					Source:           types.NamespacedName{Name: "auth", Namespace: metav1.NamespaceDefault},
					Host:             "www.auth.example.com",
					Path:             "/auth",
					Backends: []nghttpx.Backend{
						{
							Address:  "192.168.10.1",
							Port:     "80",
							Protocol: nghttpx.ProtocolH1,
							Group:    "default/alpha",
						},
					},
					RedirectIfNotTLS:         true,
					Affinity:                 nghttpx.AffinityNone,
					AffinityCookieSecure:     nghttpx.AffinityCookieSecureAuto,
					AffinityCookieStickiness: nghttpx.AffinityCookieStickinessLoose,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true

			f.svcStore = append(f.svcStore, bs1)
			f.epSliceStore = append(f.epSliceStore, bes1)
			f.gatewayClassStore = append(f.gatewayClassStore, gc)
			f.gatewayStore = append(f.gatewayStore, tt.gateways...)
			f.httpRouteStore = append(f.httpRouteStore, tt.httpRoutes...)

			f.prepare()
			f.setupStore()

			got := f.lbc.createGatewayUpstreams(context.Background(), tt.httpRoutes)

			if got, want := got, tt.want; !equality.Semantic.DeepEqual(got, want) {
				t.Errorf("got = %v, want %v", klog.Format(got), klog.Format(want))
			}
		})
	}
}

func TestHTTPRouteAccepted(t *testing.T) {
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: defaultGatewayClassController,
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	uncontrolledGC := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "uncontrolled",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "uncontrolled",
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	tests := []struct {
		desc           string
		gateways       []*gatewayv1.Gateway
		httpRoute      gatewayv1.HTTPRoute
		wantAccepted   bool
		wantRequireTLS bool
		wantHostnames  []gatewayv1.Hostname
	}{
		{
			desc: "Accept HTTPRoute",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
		},
		{
			desc: "Require TLS",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName("cert"),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("https")),
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("https")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted:   true,
			wantRequireTLS: true,
		},
		{
			desc: "Not require TLS because HTTPRoute does not refer to the section",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName("cert"),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
		},
		{
			desc: "Not require TLS because HTTPRoute refers to cleartext listener as well",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName("cert"),
										},
									},
								},
							},
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("https")),
							},
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("https")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("http")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
		},
		{
			desc: "Not require TLS because HTTPRoute refers to cleartext listener in another Gateway",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName("cert"),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw2",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("https")),
							},
							{
								Name:        "gtw2",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("https")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw2",
									SectionName: ptr.To(gatewayv1.SectionName("http")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
		},
		{
			desc: "Not accept HTTPRoute because its status is not true",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionUnknown,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "Not accept HTTPRoute because the referred Gateway status is not true",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionUnknown,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "Not accept HTTPRoute because it is not controlled by this controller",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(uncontrolledGC.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "Accept HTTPRoute even if one of the referenced Gateway is not found",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
							{
								Name: "gtw2",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
		},
		{
			desc: "With hostnames",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.com",
						"cdn.example.com",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.com",
				"www.example.com",
			},
		},
		{
			desc: "No matching hostname is dropped",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
						"cdn.example.com",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.com",
			},
		},
		{
			desc: "All hostnames are allowed because listener has no hostname",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
						"cdn.example.com",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.com",
				"www.example.net",
			},
		},
		{
			desc: "Listener hostname is used if HTTPRoute does not specify one",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"*.example.com",
			},
		},
		{
			desc: "HTTPRoute is not accepted because none of its hostnames are acceptable",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.net",
						"cdn.example.net",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "Hostnames are OR-ed across all listeners",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.net")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.com",
						"cdn.example.net",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.net",
				"www.example.com",
			},
		},
		{
			desc: "With hostnames and sectionName",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.net")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.com",
						"cdn.example.com",
						"cdn.example.net",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("http")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.com",
				"www.example.com",
			},
		},
		{
			desc: "Hostnames are OR-ed with the hostnames in the referenced listener if hostnames are not given",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.com")),
							},
							{
								Name:     "http2",
								Protocol: gatewayv1.HTTPProtocolType,
								Hostname: ptr.To(gatewayv1.Hostname("*.example.net")),
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http2")),
							},
						},
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("http")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("http2")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"*.example.com",
				"*.example.net",
			},
		},
		{
			desc: "All hostnames are acceptable if the referenced listener has no hostname",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name:        "gtw",
								SectionName: ptr.To(gatewayv1.SectionName("http")),
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.com",
						"cdn.example.com",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name:        "gtw",
									SectionName: ptr.To(gatewayv1.SectionName("http")),
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.com",
				"www.example.com",
			},
		},
		{
			desc: "Duplicated hostnames are discarded",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw2",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "http",
								Protocol: gatewayv1.HTTPProtocolType,
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			httpRoute: gatewayv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "route",
					Namespace: "ns",
				},
				Spec: gatewayv1.HTTPRouteSpec{
					CommonRouteSpec: gatewayv1.CommonRouteSpec{
						ParentRefs: []gatewayv1.ParentReference{
							{
								Name: "gtw",
							},
							{
								Name: "gtw2",
							},
						},
					},
					Hostnames: []gatewayv1.Hostname{
						"www.example.com",
						"cdn.example.com",
					},
				},
				Status: gatewayv1.HTTPRouteStatus{
					RouteStatus: gatewayv1.RouteStatus{
						Parents: []gatewayv1.RouteParentStatus{
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
							{
								ParentRef: gatewayv1.ParentReference{
									Name: "gtw2",
								},
								ControllerName: defaultGatewayClassController,
								Conditions: []metav1.Condition{
									{
										Type:   string(gatewayv1.RouteConditionAccepted),
										Status: metav1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
			wantAccepted: true,
			wantHostnames: []gatewayv1.Hostname{
				"cdn.example.com",
				"www.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true

			f.gatewayClassStore = append(f.gatewayClassStore, gc, uncontrolledGC)
			f.gatewayStore = append(f.gatewayStore, tt.gateways...)
			f.httpRouteStore = append(f.httpRouteStore, &tt.httpRoute)

			f.prepare()
			f.setupStore()

			accepted, requireTLS, hostnames := f.lbc.httpRouteAccepted(context.Background(), &tt.httpRoute)

			if got, want := accepted, tt.wantAccepted; got != want {
				t.Errorf("accepted = %v, want %v", got, want)
			}

			if got, want := requireTLS, tt.wantRequireTLS; got != want {
				t.Errorf("requireTLS = %v, want %v", got, want)
			}

			if got, want := hostnames, tt.wantHostnames; !slices.Equal(got, want) {
				t.Errorf("hostnames = %v, want %v", got, want)
			}
		})
	}
}

func TestCreateGatewayCredentials(t *testing.T) {
	gc := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gc",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: defaultGatewayClassController,
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	uncontrolledGC := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "uncontrolled",
		},
		Spec: gatewayv1.GatewayClassSpec{
			ControllerName: "uncontrolled",
		},
		Status: gatewayv1.GatewayClassStatus{
			Conditions: defaultGatewayClassConditions,
		},
	}

	cert1 := newTLSSecret("ns", "cert1", []byte(tlsCrt), []byte(tlsKey))
	cert2 := newTLSSecret("ns", "cert2", []byte(tlsCrt), []byte(tlsKey))

	certChecksum, err := hex.DecodeString("f2eae056c5e1c8153ba7b4d1a78f6ec6a9ef8c2e9dfcecd407033e2562207716")
	if err != nil {
		panic(err)
	}

	keyChecksum, err := hex.DecodeString("c70eb70e955140a677869e4a7db443a699a350a5173d412aad3c8dc4c06592f5")
	if err != nil {
		panic(err)
	}

	tests := []struct {
		desc     string
		gateways []*gatewayv1.Gateway
		want     []*nghttpx.TLSCred
	}{
		{
			desc: "No Gateways",
		},
		{
			desc: "Create TLSCred",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName(cert1.Name),
										},
										{
											Name: gatewayv1.ObjectName(cert2.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			want: []*nghttpx.TLSCred{
				{
					Name: cert1.Namespace + "/" + cert1.Name,
					Key: nghttpx.PrivateChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(keyChecksum) + ".key",
						Content:  cert1.Data[corev1.TLSPrivateKeyKey],
						Checksum: keyChecksum,
					},
					Cert: nghttpx.ChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(certChecksum) + ".crt",
						Content:  cert1.Data[corev1.TLSCertKey],
						Checksum: certChecksum,
					},
				},
				{
					Name: cert2.Namespace + "/" + cert2.Name,
					Key: nghttpx.PrivateChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(keyChecksum) + ".key",
						Content:  cert2.Data[corev1.TLSPrivateKeyKey],
						Checksum: keyChecksum,
					},
					Cert: nghttpx.ChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(certChecksum) + ".crt",
						Content:  cert2.Data[corev1.TLSCertKey],
						Checksum: certChecksum,
					},
				},
			},
		},
		{
			desc: "Ignore certificate from a resource which is not Secret",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Kind: ptr.To(gatewayv1.Kind("ConfigMap")),
											Name: gatewayv1.ObjectName(cert1.Name),
										},
										{
											Name: gatewayv1.ObjectName(cert2.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			want: []*nghttpx.TLSCred{
				{
					Name: cert2.Namespace + "/" + cert2.Name,
					Key: nghttpx.PrivateChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(keyChecksum) + ".key",
						Content:  cert2.Data[corev1.TLSPrivateKeyKey],
						Checksum: keyChecksum,
					},
					Cert: nghttpx.ChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(certChecksum) + ".crt",
						Content:  cert2.Data[corev1.TLSCertKey],
						Checksum: certChecksum,
					},
				},
			},
		},
		{
			desc: "Ignore certificate from a Secret which belongs to the different namespace",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name:      gatewayv1.ObjectName(cert1.Name),
											Namespace: ptr.To(gatewayv1.Namespace("other")),
										},
										{
											Name: gatewayv1.ObjectName(cert2.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			want: []*nghttpx.TLSCred{
				{
					Name: cert2.Namespace + "/" + cert2.Name,
					Key: nghttpx.PrivateChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(keyChecksum) + ".key",
						Content:  cert2.Data[corev1.TLSPrivateKeyKey],
						Checksum: keyChecksum,
					},
					Cert: nghttpx.ChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(certChecksum) + ".crt",
						Content:  cert2.Data[corev1.TLSCertKey],
						Checksum: certChecksum,
					},
				},
			},
		},
		{
			desc: "Ignore certificate from Gateway which has not been accepted yet",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName(cert1.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw-not-accepted",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName(cert2.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionUnknown,
							},
						},
					},
				},
			},
			want: []*nghttpx.TLSCred{
				{
					Name: cert1.Namespace + "/" + cert1.Name,
					Key: nghttpx.PrivateChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(keyChecksum) + ".key",
						Content:  cert1.Data[corev1.TLSPrivateKeyKey],
						Checksum: keyChecksum,
					},
					Cert: nghttpx.ChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(certChecksum) + ".crt",
						Content:  cert1.Data[corev1.TLSCertKey],
						Checksum: certChecksum,
					},
				},
			},
		},
		{
			desc: "Ignore certificate from Gateway which has not been accepted yet",
			gateways: []*gatewayv1.Gateway{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(gc.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName(cert1.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gtw-not-controlled",
						Namespace: "ns",
					},
					Spec: gatewayv1.GatewaySpec{
						GatewayClassName: gatewayv1.ObjectName(uncontrolledGC.Name),
						Listeners: []gatewayv1.Listener{
							{
								Name:     "https",
								Protocol: gatewayv1.HTTPSProtocolType,
								TLS: &gatewayv1.GatewayTLSConfig{
									CertificateRefs: []gatewayv1.SecretObjectReference{
										{
											Name: gatewayv1.ObjectName(cert2.Name),
										},
									},
								},
							},
						},
					},
					Status: gatewayv1.GatewayStatus{
						Conditions: []metav1.Condition{
							{
								Type:   string(gatewayv1.GatewayConditionAccepted),
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			want: []*nghttpx.TLSCred{
				{
					Name: cert1.Namespace + "/" + cert1.Name,
					Key: nghttpx.PrivateChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(keyChecksum) + ".key",
						Content:  cert1.Data[corev1.TLSPrivateKeyKey],
						Checksum: keyChecksum,
					},
					Cert: nghttpx.ChecksumFile{
						Path:     "conf/tls/" + hex.EncodeToString(certChecksum) + ".crt",
						Content:  cert1.Data[corev1.TLSCertKey],
						Checksum: certChecksum,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true

			f.secretStore = append(f.secretStore, cert1, cert2)
			f.gatewayClassStore = append(f.gatewayClassStore, gc, uncontrolledGC)
			f.gatewayStore = append(f.gatewayStore, tt.gateways...)

			f.prepare()
			f.setupStore()

			tlsCreds := f.lbc.createGatewayCredentials(context.Background(), tt.gateways)

			if got, want := tlsCreds, tt.want; !equality.Semantic.DeepEqual(got, want) {
				t.Errorf("tlsCreds = %v, want %v", klog.Format(got), klog.Format(want))
			}
		})
	}
}
