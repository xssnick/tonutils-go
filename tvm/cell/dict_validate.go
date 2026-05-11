package cell

import "fmt"

// validatePlainDictRoot applies strict HashmapE/Hashmap validation.
// Augmented dictionaries must use the dedicated LoadAugDict/ToAugDict path.
func validatePlainDictRoot(root *Cell, keySz uint) error {
	if root == nil {
		return nil
	}
	return validatePlainDictNode(root, keySz)
}

func validatePlainDictNode(c *Cell, keySz uint) error {
	if c == nil {
		return fmt.Errorf("dict branch is nil")
	}

	node, err := parseFixedDictNode(c, keySz, nil)
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
