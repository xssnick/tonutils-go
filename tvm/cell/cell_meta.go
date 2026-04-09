package cell

func (c *Cell) IsSpecial() bool {
	return c.special
}

func (c *Cell) Level() int {
	return c.levelMask.GetLevel()
}

func (c *Cell) LevelMask() LevelMask {
	return c.levelMask
}
