package cell

func (c *Cell) Level() int {
	return c.getLevelMask().GetLevel()
}

func (c *Cell) LevelMask() LevelMask {
	return c.getLevelMask()
}
