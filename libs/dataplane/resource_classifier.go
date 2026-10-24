package dataplane

import "fmt"

// Classification defines the resource used to classify traffic through Classifier or other mechanism
// by tagging skb->mark in the packet. For the linux implementation, this is done
// by nftables. We use `google/nftables` low-level netfilter library to communicate
// directly with the netlink socket instead of depending on nft binary.
//
// NFTABLES equivalent commands that this sets up:
//
//	nft 'add element inet maeto_class dscpmark { <.dscp>: <.mark> }'
type Classification struct {
	Classifier uint32 // 6-bit for DSCP
	Mark       uint32 // https://www.man7.org/linux/man-pages/man8/tc-skbedit.8.html
}

func (c Classification) ID() ID {
	return ID{
		Kind: KindClassification,
		Key:  fmt.Sprintf("%s:%d.%d", KindClassification, c.Classifier, c.Mark),
	}
}

func (c Classification) CompareTo(actual Resource) Action {
	return ActionNone
}

func (c Classification) Type() string { return "classification" }
