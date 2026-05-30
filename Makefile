all: hamirc hamirc.exe

hamirc:
	CGO_ENABLED=0 GOOS=linux go build

hamirc.exe:
	GOOS=windows go build

clean:
	rm -f hamirc hamirc.exe