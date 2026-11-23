//go:build !windows

package hwinfo

// Read always fails on systems other than Windows, where HWiNFO does not
// exist. Parsing and filtering work everywhere, which is what the tests use.
func (r Reader) Read() (data []byte, locked bool, err error) {
	return nil, false, ErrUnsupported
}
