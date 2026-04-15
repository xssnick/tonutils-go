package cell

func (c *Cell) IsSpecial() bool {
	return c.isSpecial()
}

func (c *Cell) Level() int {
	return c.getLevelMask().GetLevel()
}

func (c *Cell) LevelMask() LevelMask {
	return c.getLevelMask()
}
