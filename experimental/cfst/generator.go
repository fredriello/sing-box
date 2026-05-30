package cfst

import (
	"context"
	"fmt"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/cfess"
)

// RenderTag renders a tag template with the given parameters.
// Supported placeholders: {{base}}, {{colo}}, {{colo_lower}}, {{index}}
func RenderTag(template, base, colo string, index int) string {
	if template == "" {
		template = "{{base}}-{{colo_lower}}-{{index}}"
	}
	result := template
	result = strings.ReplaceAll(result, "{{base}}", base)
	result = strings.ReplaceAll(result, "{{colo}}", colo)
	result = strings.ReplaceAll(result, "{{colo_lower}}", strings.ToLower(colo))
	result = strings.ReplaceAll(result, "{{index}}", fmt.Sprintf("%d", index))
	return result
}

// GenerateOutbounds creates dynamic outbounds from CFST results for a given cfess mapping.
func (s *CFSTService) GenerateOutbounds(mapping option.CFESSMapping, results []Result) error {
	// 1. Remove old generated outbounds for this mapping
	s.mu.Lock()
	oldTags := s.generatedTags[mapping.Tag]
	oldGroupTag := s.groupTags[mapping.Tag]
	delete(s.generatedTags, mapping.Tag)
	delete(s.groupTags, mapping.Tag)
	s.mu.Unlock()

	// Remove old outbounds (outside lock)
	if oldGroupTag != "" {
		_ = s.outbound.Remove(oldGroupTag)
	}
	for _, tag := range oldTags {
		_ = s.outbound.Remove(tag)
	}

	// 2. Create new outbounds (outside lock)
	count := mapping.GeneratedCount
	if count <= 0 || count > len(results) {
		count = len(results)
	}

	// Look up the base cfess outbound
	baseOutbound, found := s.outbound.Outbound(mapping.Tag)
	if !found {
		s.logger.Warn("base cfess outbound not found: ", mapping.Tag)
		return fmt.Errorf("base cfess outbound not found: %s", mapping.Tag)
	}

	cfessOutbound, ok := baseOutbound.(*cfess.Outbound)
	if !ok {
		s.logger.Warn("outbound is not cfess type: ", mapping.Tag)
		return fmt.Errorf("outbound is not cfess type: %s", mapping.Tag)
	}

	baseOptions := cfessOutbound.Options
	var generatedTags []string

	// Track per-colo index for tag rendering
	coloIndex := make(map[string]int)

	for _, result := range results[:count] {
		coloIndex[result.Colo]++
		idx := coloIndex[result.Colo]
		tag := RenderTag(mapping.TagTemplate, mapping.Tag, result.Colo, idx)

		// Create VLESSOutboundOptions with replaced server IP
		vlessOptions := option.VLESSOutboundOptions{
			DialerOptions:               baseOptions.DialerOptions,
			ServerOptions:               baseOptions.ServerOptions,
			UUID:                        baseOptions.UUID,
			Flow:                        baseOptions.Flow,
			Network:                     baseOptions.Network,
			OutboundTLSOptionsContainer: baseOptions.OutboundTLSOptionsContainer,
			Multiplex:                   baseOptions.Multiplex,
			Transport:                   baseOptions.Transport,
			PacketEncoding:              baseOptions.PacketEncoding,
		}
		// Replace server with result IP (keep original port)
		vlessOptions.ServerOptions.Server = result.IP

		// Create the outbound via outbound manager
		err := s.outbound.Create(
			context.Background(),
			s.router,
			s.logger,
			tag,
			C.TypeVLESS,
			&vlessOptions,
		)
		if err != nil {
			s.logger.Warn("failed to create outbound ", tag, ": ", err)
			continue
		}
		generatedTags = append(generatedTags, tag)
	}

	// Create/update urltest group
	groupTag := mapping.GroupTag
	if groupTag == "" {
		groupTag = mapping.Tag + "-group"
	}

	var groupOutbounds []string
	if mapping.IncludeOriginal {
		groupOutbounds = append(groupOutbounds, mapping.Tag)
	}
	groupOutbounds = append(groupOutbounds, generatedTags...)

	if len(groupOutbounds) > 0 {
		urlTestOptions := &option.URLTestOutboundOptions{
			Outbounds: groupOutbounds,
		}
		err := s.outbound.Create(
			context.Background(),
			s.router,
			s.logger,
			groupTag,
			C.TypeURLTest,
			urlTestOptions,
		)
		if err != nil {
			s.logger.Warn("failed to create/update urltest group ", groupTag, ": ", err)
			return fmt.Errorf("failed to create urltest group %s: %w", groupTag, err)
		}
	}

	// 3. Store new tags (under lock)
	s.mu.Lock()
	s.generatedTags[mapping.Tag] = generatedTags
	s.groupTags[mapping.Tag] = groupTag
	s.mu.Unlock()

	return nil
}

// GenerateAllOutbounds generates outbounds for all configured CFESS mappings.
func (s *CFSTService) GenerateAllOutbounds(results []Result) {
	if s.outbound == nil {
		return
	}
	for _, mapping := range s.options.CFESS {
		err := s.GenerateOutbounds(mapping, results)
		if err != nil {
			s.logger.Warn("generate outbounds for ", mapping.Tag, " failed: ", err)
		}
	}
}
