package cfess

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/vless"
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.CFESSOutboundOptions](registry, C.TypeCFESS, NewOutbound)
}

type Outbound struct {
	adapter.Outbound
	Options option.CFESSOutboundOptions
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.CFESSOutboundOptions) (adapter.Outbound, error) {
	vlessOptions := option.VLESSOutboundOptions{
		DialerOptions:               options.DialerOptions,
		ServerOptions:               options.ServerOptions,
		UUID:                        options.UUID,
		Flow:                        options.Flow,
		Network:                     options.Network,
		OutboundTLSOptionsContainer: options.OutboundTLSOptionsContainer,
		Multiplex:                   options.Multiplex,
		Transport:                   options.Transport,
		PacketEncoding:              options.PacketEncoding,
	}
	inner, err := vless.NewOutbound(ctx, router, logger, tag, vlessOptions)
	if err != nil {
		return nil, err
	}
	return &Outbound{
		Outbound: inner,
		Options:  options,
	}, nil
}
