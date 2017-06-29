/**
 * Copyright 2017, Z Lab Corporation. All rights reserved.
 * Copyright 2017, nghttpx Ingress controller contributors
 *
 * For the full copyright and license information, please view the LICENSE
 * file that was distributed with this source code.
 */

package controller

import (
	"reflect"
	"testing"

	"k8s.io/client-go/pkg/api/v1"
)

// TestSortLoadBalancerIngress verifies that sortLoadBalancerIngress sorts given items.
func TestSortLoadBalancerIngress(t *testing.T) {
	input := []v1.LoadBalancerIngress{
		{IP: "delta", Hostname: "alpha"},
		{IP: "alpha", Hostname: "delta"},
		{IP: "alpha", Hostname: "charlie"},
		{IP: "bravo", Hostname: ""},
	}

	ans := []v1.LoadBalancerIngress{
		{IP: "alpha", Hostname: "charlie"},
		{IP: "alpha", Hostname: "delta"},
		{IP: "bravo", Hostname: ""},
		{IP: "delta", Hostname: "alpha"},
	}

	sortLoadBalancerIngress(input)

	if got, want := input, ans; !reflect.DeepEqual(got, want) {
		t.Errorf("sortLoadBalancerIngress(...) = %+v, want %+v", got, want)
	}
}

// TestUniqLoadBalancerIngress verifies that uniqLoadBalancerIngress removes duplicated items.
func TestUniqLoadBalancerIngress(t *testing.T) {
	tests := []struct {
		input []v1.LoadBalancerIngress
		ans   []v1.LoadBalancerIngress
	}{
		{
			input: nil,
			ans:   nil,
		},
		{
			input: []v1.LoadBalancerIngress{
				{IP: "alpha", Hostname: "bravo"},
				{IP: "alpha", Hostname: "bravo"},
				{IP: "bravo", Hostname: "alpha"},
				{IP: "delta", Hostname: ""},
				{IP: "delta", Hostname: ""},
			},
			ans: []v1.LoadBalancerIngress{
				{IP: "alpha", Hostname: "bravo"},
				{IP: "bravo", Hostname: "alpha"},
				{IP: "delta", Hostname: ""},
			},
		},
	}

	for i, tt := range tests {
		if got, want := uniqLoadBalancerIngress(tt.input), tt.ans; !reflect.DeepEqual(got, want) {
			t.Errorf("#%v: uniqLoadBalancerIngress(...) = %+v, want %+v", i, got, want)
		}
	}
}

// TestRemoveAddressFromLoadBalancerIngress verifies that removeAddressFromLoadBalancerIngress removes given address.
func TestUtilRemoveAddressFromLoadBalancerIngress(t *testing.T) {
	tests := []struct {
		addr  string
		input []v1.LoadBalancerIngress
		ans   []v1.LoadBalancerIngress
	}{
		{
			addr:  "alpha",
			input: nil,
			ans:   nil,
		},
		{
			addr: "alpha",
			input: []v1.LoadBalancerIngress{
				{IP: "alpha"},
				{IP: "bravo"},
				{Hostname: "alpha"},
				{IP: "charlie"},
				{IP: "alpha"},
			},
			ans: []v1.LoadBalancerIngress{
				{IP: "bravo"},
				{IP: "charlie"},
			},
		},
		{
			addr: "alpha",
			input: []v1.LoadBalancerIngress{
				{IP: "bravo"},
				{IP: "charlie"},
			},
			ans: []v1.LoadBalancerIngress{
				{IP: "bravo"},
				{IP: "charlie"},
			},
		},
		{
			addr: "alpha",
			input: []v1.LoadBalancerIngress{
				{IP: "alpha"},
			},
			ans: []v1.LoadBalancerIngress{},
		},
	}

	for i, tt := range tests {
		if got, want := removeAddressFromLoadBalancerIngress(tt.input, tt.addr), tt.ans; !reflect.DeepEqual(got, want) {
			t.Errorf("#%v: removeAddressFromLoadBalancerIngress(...) = %+v, want %+v", i, got, want)
		}
	}
}
