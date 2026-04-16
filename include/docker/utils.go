package docker

func GetContainerName(userId, containerId string) string {
	return userId + "." + containerId
}
