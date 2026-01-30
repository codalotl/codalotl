package mypkg

func Average(values []Temperature) Temperature {
	if len(values) == 0 {
		return 0
	}
	sum := sumTemp(values)
	return sum / Temperature(len(values))
}

func sumTemp(values []Temperature) Temperature {
	sum := 0
	for _, v := range values {
		sum += int(v)
	}
	return Temperature(sum)
}
