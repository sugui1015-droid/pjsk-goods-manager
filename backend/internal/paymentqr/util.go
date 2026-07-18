package paymentqr

import "io"

// readAllLimited reads at most limit bytes from r. If the stream reaches limit it
// is treated as over-limit (the caller passes MaxImageBytes+1 as limit, so
// hitting limit means the payload exceeded MaxImageBytes). It never allocates
// more than limit bytes.
func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= limit {
		return nil, ErrImageTooLarge
	}
	return data, nil
}
