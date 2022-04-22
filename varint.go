func varint(arr []byte) {
	var x int64
	for c, v := range arr {
		if v&0x80 == 0 {
			x |= int64(v) << (c * 7)
			fmt.Println("--->", x)
			return
		} else {
			x |= int64(v&0x7F) << (c * 7)
		}
	}
}
