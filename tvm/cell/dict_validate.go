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

	if c.special {
		if c.GetType() == PrunedCellType {
			return nil
		}
		return fmt.Errorf("dict has unsupported special cell in tree structure")
	}

	loader := c.BeginParse()
	labelLen, _, err := loadLabel(keySz, loader, BeginCell())
	if err != nil {
		return fmt.Errorf("failed to parse dict label: %w", err)
	}

	if labelLen < keySz {
		if loader.BitsLeft() != 0 || loader.RefsNum() != 2 {
			return fmt.Errorf("invalid dict fork node: %d data bits left, %d refs left after %d-bit label with %d remaining key bits",
				loader.BitsLeft(), loader.RefsNum(), labelLen, keySz)
		}

		nextKeySz := keySz - labelLen - 1

		left, err := loader.LoadRefCell()
		if err != nil {
			return fmt.Errorf("failed to load left branch: %w", err)
		}
		if err = validatePlainDictNode(left, nextKeySz); err != nil {
			return fmt.Errorf("invalid left branch: %w", err)
		}

		right, err := loader.LoadRefCell()
		if err != nil {
			return fmt.Errorf("failed to load right branch: %w", err)
		}
		if err = validatePlainDictNode(right, nextKeySz); err != nil {
			return fmt.Errorf("invalid right branch: %w", err)
		}
	}

	return nil
}
