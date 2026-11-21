package domain

import "strconv"

// ExternalDocumentSourceType is persisted as its numeric value. Append new
// values; never renumber an existing source.
type ExternalDocumentSourceType uint16

const (
	ExternalDocumentSourceTypeOpenList ExternalDocumentSourceType = 1
	// ExternalDocumentSourceTypeXueqiu is one original post pulled from the
	// Xueqiu wire desk (xueqiu-tool) ACK queue.
	ExternalDocumentSourceTypeXueqiu ExternalDocumentSourceType = 2
	// ExternalDocumentSourceTypeTHS is one post pulled from the Tonghuashun
	// wire desk (ths-wire-desk) ACK queue.
	ExternalDocumentSourceTypeTHS ExternalDocumentSourceType = 3
)

func (t ExternalDocumentSourceType) Valid() bool {
	return t == ExternalDocumentSourceTypeOpenList || t == ExternalDocumentSourceTypeXueqiu || t == ExternalDocumentSourceTypeTHS
}

func (t ExternalDocumentSourceType) String() string {
	switch t {
	case ExternalDocumentSourceTypeOpenList:
		return "OPENLIST"
	case ExternalDocumentSourceTypeXueqiu:
		return "XUEQIU"
	case ExternalDocumentSourceTypeTHS:
		return "THS"
	default:
		return "UNKNOWN_" + strconv.FormatUint(uint64(t), 10)
	}
}

// ExternalDocumentIngestionStatus is the durable state of one remote object
// version. The row exists before documents.id is available, so download and
// retry failures remain auditable.
type ExternalDocumentIngestionStatus uint8

const (
	ExternalDocumentIngestionStatusDiscovered  ExternalDocumentIngestionStatus = 1
	ExternalDocumentIngestionStatusDownloading ExternalDocumentIngestionStatus = 2
	ExternalDocumentIngestionStatusIngested    ExternalDocumentIngestionStatus = 3
	ExternalDocumentIngestionStatusAnalyzing   ExternalDocumentIngestionStatus = 4
	ExternalDocumentIngestionStatusSucceeded   ExternalDocumentIngestionStatus = 5
	ExternalDocumentIngestionStatusFailed      ExternalDocumentIngestionStatus = 6
)

func (s ExternalDocumentIngestionStatus) Terminal() bool {
	return s == ExternalDocumentIngestionStatusSucceeded
}

func (s ExternalDocumentIngestionStatus) Valid() bool {
	return s >= ExternalDocumentIngestionStatusDiscovered && s <= ExternalDocumentIngestionStatusFailed
}
