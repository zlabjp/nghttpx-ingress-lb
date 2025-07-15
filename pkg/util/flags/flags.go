package flags

import (
	"errors"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

type NamespacedName types.NamespacedName

func (n *NamespacedName) String() string {
	if n.Namespace == "" && n.Name == "" {
		return ""
	}

	return types.NamespacedName(*n).String()
}

func (n *NamespacedName) Set(v string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(v)
	if err != nil {
		return err
	}

	if ns == "" {
		return errors.New("namespace must not be empty")
	}

	if name == "" {
		return errors.New("name must not be empty")
	}

	n.Name = name
	n.Namespace = ns

	return nil
}

func (n *NamespacedName) Type() string {
	return "namespacedName"
}
