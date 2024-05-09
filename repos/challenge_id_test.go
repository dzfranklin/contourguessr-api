package repos

import "testing"

func TestChallengeID(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			encoded := encodeChallengeID(i)
			decoded, err := decodeChallengeID(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if i != decoded {
				t.Fatalf("expected %d, got %d", i, decoded)
			}
		}
	})

	t.Run("samples", func(t *testing.T) {
		tests := []struct {
			decoded int
			encoded string
		}{
			{1, "ae"},
			{42, "fi"},
			{2048, "baaa"},
		}
		for _, test := range tests {
			t.Logf("testing %+v", test)
			encoded := encodeChallengeID(test.decoded)
			if encoded != test.encoded {
				t.Errorf("encode: expected %s, got %s", test.encoded, encoded)
			}
			decoded, err := decodeChallengeID(test.encoded)
			if err != nil {
				t.Error(err)
			} else if test.decoded != decoded {
				t.Errorf("decode: expected %d, got %d", test.decoded, decoded)
			}
		}
	})
}
