package controller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
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

			err := f.lc.syncGatewayClass(context.Background(), tt.gatewayClass.Name)
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

			key := types.NamespacedName{Name: tt.gateway.Name, Namespace: tt.gateway.Namespace}.String()

			err := f.lc.syncGateway(context.Background(), key)
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
		gateway          gatewayv1.Gateway
		httpRoute        gatewayv1.HTTPRoute
		noUpdate         bool
		wantParentStatus []gatewayv1.RouteParentStatus
	}{
		{
			desc: "Accept HTTPRoute",
			gateway: gatewayv1.Gateway{
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
			gateway: gatewayv1.Gateway{
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
			gateway: gatewayv1.Gateway{
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
			gateway: gatewayv1.Gateway{
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
			gateway: gatewayv1.Gateway{
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
			gateway: gatewayv1.Gateway{
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
			gateway: gatewayv1.Gateway{
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
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			f := newFixture(t)
			f.gatewayAPI = true
			f.currentTime = ts

			f.gatewayClassStore = append(f.gatewayClassStore, gc, uncontrolledGC)
			f.gatewayStore = append(f.gatewayStore, &tt.gateway)
			f.httpRouteStore = append(f.httpRouteStore, &tt.httpRoute)
			f.gatewayObjects = append(f.gatewayObjects, &tt.httpRoute)

			f.prepare()
			f.setupStore()

			key := types.NamespacedName{Name: tt.httpRoute.Name, Namespace: tt.httpRoute.Namespace}.String()

			err := f.lc.syncHTTPRoute(context.Background(), key)
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
