package cell

import "fmt"

const maxDictKeyBits = 1023

func validateDictKeySize(keySz uint) error {
	if keySz > maxDictKeyBits {
		return fmt.Errorf("dict key size exceeds %d bits", maxDictKeyBits)
	}
	return nil
}

// validatePlainDictRoot applies strict HashmapE/Hashmap validation.
// Augmented dictionaries must use the dedicated LoadAugDict/ToAugDict path.
func validatePlainDictRoot(root *Cell, keySz uint) error {
	if root == nil {
		return validateDictKeySize(keySz)
	}
	if err := validateDictKeySize(keySz); err != nil {
		return err
	}
	return validatePlainDictNode(root.WithTrace(nil), keySz)
}

func validatePlainDictNode(c *Cell, keySz uint) error {
	if c == nil {
		return fmt.Errorf("dict branch is nil")
	}

	node, err := parseFixedDictNode(c, keySz)
	if err != nil {
		return err
	}

	if pruned, err := node.prunedBoundary("dict"); err != nil || pruned {
		return err
	}

	if !node.isLeaf(keySz) {
		if node.loader.BitsLeft() != 0 || node.loader.RefsNum() != 2 {
			return fmt.Errorf("invalid dict fork node: %d data bits left, %d refs left after %d-bit label with %d remaining key bits",
				node.loader.BitsLeft(), node.loader.RefsNum(), node.labelLen, keySz)
		}

		nextKeySz := node.nextKeyBits(keySz)

		left, err := node.boundaryRef(0)
		if err != nil {
			return fmt.Errorf("failed to load left branch: %w", err)
		}
		if err = validatePlainDictNode(left, nextKeySz); err != nil {
			return fmt.Errorf("invalid left branch: %w", err)
		}

		right, err := node.boundaryRef(1)
		if err != nil {
			return fmt.Errorf("failed to load right branch: %w", err)
		}
		if err = validatePlainDictNode(right, nextKeySz); err != nil {
			return fmt.Errorf("invalid right branch: %w", err)
		}
	}

	return nil
}
