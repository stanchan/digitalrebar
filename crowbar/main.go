package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	crowbar "github.com/VictorLowther/crowbar-api"
	"github.com/VictorLowther/jsonpatch"
	"github.com/VictorLowther/jsonpatch/utils"
	"github.com/spf13/cobra"
)

var (
	debug              = false
	endpoint           = "http://127.0.0.1:3000"
	username, password string
	app                = &cobra.Command{
		Use:   "crowbar",
		Short: "A CLI application for interacting with the Crowbar API",
	}
)

func d(msg string, args ...interface{}) {
	if debug {
		log.Printf(msg, args...)
	}
}

func prettyJSON(o interface{}) (res string) {
	buf, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		log.Fatalf("Failed to unmarshal returned object!")
	}
	return string(buf)
}

func init() {
	if ep := os.Getenv("CROWBAR_ENDPOINT"); ep != "" {
		endpoint = ep
	}
	if kv := os.Getenv("CROWBAR_KEY"); kv != "" {
		key := strings.SplitN(kv, ":", 2)
		if len(key) < 2 {
			log.Fatal("CROWBAR_KEY does not contain a username:password pair!")
		}
		if key[0] == "" || key[1] == "" {
			log.Fatal("CROWBAR_KEY contains an invalid username:password pair!")
		}
		username = key[0]
		password = key[1]
	}
	app.PersistentFlags().StringVarP(&endpoint,
		"endpoint", "E", endpoint,
		"The Crowbar API endpoint to talk to")
	app.PersistentFlags().StringVarP(&username,
		"username", "U", username,
		"Name of the Crowbar user to talk to")
	app.PersistentFlags().StringVarP(&password,
		"password", "P", password,
		"Password of the Crowbar user")
	app.PersistentFlags().BoolVarP(&debug,
		"debug", "d", false,
		"Whether the CLI should run in debug mode")
}

func makeCommandTree(singularName string,
	maker func() crowbar.Crudder) (res *cobra.Command) {
	name := singularName + "s"
	d("Making command tree for %v\n", name)
	res = &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Access CLI commands relating to %v", name),
	}
	commands := make([]*cobra.Command, 8)
	commands[0] = &cobra.Command{
		Use:   "list",
		Short: fmt.Sprintf("List all %v", name),
		Run: func(c *cobra.Command, args []string) {
			objs := []interface{}{}
			if err := crowbar.List(maker().ApiName(), &objs); err != nil {
				log.Fatalf("Error listing %v: %v", name, err.Error())
			}
			fmt.Println(prettyJSON(objs))
		},
	}
	commands[1] = &cobra.Command{
		Use:   "match [json]",
		Short: fmt.Sprintf("List all %v that match the template in [json]", name),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 1 {
				log.Fatalf("%v requires 1 argument\n", c.UseLine())
			}
			objs := []interface{}{}
			vals := map[string]interface{}{}
			if err := json.Unmarshal([]byte(args[0]), &vals); err != nil {
				log.Fatalf("Matches not valid JSON\n%v", err)
			}
			if err := crowbar.Match(maker().ApiName(), vals, &objs); err != nil {
				log.Fatalf("Error getting matches for %v\nError:%v\n", singularName, err.Error())
			}
			fmt.Println(prettyJSON(objs))
		},
	}
	commands[2] = &cobra.Command{
		Use:   "show [id]",
		Short: fmt.Sprintf("Show a single %v by id", singularName),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 1 {
				log.Fatalf("%v requires 1 argument\n", c.UseLine())
			}
			obj := maker()
			if crowbar.SetId(obj, args[0]) != nil {
				log.Fatalf("Failed to parse ID %v for an %v\n", singularName, args[0])
			}
			if err := crowbar.Read(obj); err != nil {
				log.Fatalf("Unable to load %v %v\n", singularName, obj)
			}
			fmt.Println(prettyJSON(obj))
		},
	}
	commands[3] = &cobra.Command{
		Use:   "sample",
		Short: fmt.Sprintf("Get the default values for a %v", singularName),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 0 {
				log.Fatalf("%v takes no arguments", c.UseLine())
			}
			obj := maker()
			if err := crowbar.Init(obj); err != nil {
				log.Fatalf("Unable to fetch defaults for %v: %v\n", singularName, err.Error())
			}
			fmt.Println(prettyJSON(obj))
		},
	}
	commands[4] = &cobra.Command{
		Use:   "create [json]",
		Short: fmt.Sprintf("Create a new %v with the passed-in JSON", singularName),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 1 {
				log.Fatalf("%v requires 1 argument\n", c.UseLine())
			}
			obj := maker()
			if err := crowbar.CreateJSON(obj, []byte(args[0])); err != nil {
				log.Fatalf("Unable to create new %v: %v\n", singularName, err.Error())
			}
			fmt.Println(prettyJSON(obj))
		},
	}
	commands[5] = &cobra.Command{
		Use:   "update [id] [json]",
		Short: fmt.Sprintf("Unsafely update %v by id with the passed-in JSON", singularName),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 2 {
				log.Fatalf("%v requires 2 arguments\n", c.UseLine())
			}
			obj := maker()
			if err := crowbar.SetId(obj, args[0]); err != nil {
				log.Fatalf("Failed to parse ID %v for a %v\n%v\n", args[0], singularName, err)
			}
			if err := crowbar.Read(obj); err != nil {
				log.Fatalf("Failed to fetch %v from the server\n%v\n", args[0], err)
			}
			intermediate, err := json.Marshal(obj)
			if err != nil {
				log.Fatalf("Unable to marshal %v\n%v", args[0], err)
			}
			merged, err := utils.MergeJSON(intermediate, []byte(args[1]))
			if err != nil {
				log.Fatalf("Unable to generate merged JSON\n%v", err)
			}
			patch, err := jsonpatch.GenerateJSON(intermediate, merged, true)
			if err != nil {
				log.Fatalf("Cannot generate JSON Patch\n%v\n", err)
			}

			if err := crowbar.Patch(obj, patch); err != nil {
				log.Fatalf("Unable to patch %v\n%v\n", args[0], err)
			}

			fmt.Println(prettyJSON(obj))
		},
	}
	commands[6] = &cobra.Command{
		Use:   "patch [objectJson] [changesJson]",
		Short: fmt.Sprintf("Patch %v with the passed-in JSON", singularName),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 2 {
				log.Fatalf("%v requires 2 arguments\n", c.UseLine())
			}
			obj := maker()
			if err := json.Unmarshal([]byte(args[0]), obj); err != nil {
				log.Fatalf("Unable to parse %v JSON %v\nError: %v\n", args[0], err)
			}
			newObj := maker()
			json.Unmarshal([]byte(args[0]), newObj)
			if err := json.Unmarshal([]byte(args[1]), newObj); err != nil {
				log.Fatalf("Unable to parse %v JSON %v\nError: %v\n", args[1], err)
			}
			newBuf, _ := json.Marshal(newObj)
			patch, err := jsonpatch.GenerateJSON([]byte(args[0]), newBuf, true)
			if err != nil {
				log.Fatalf("Cannot generate JSON Patch\n%v\n", err)
			}

			if err := crowbar.Patch(obj, patch); err != nil {
				log.Fatalf("Unable to patch %v\n%v\n", args[0], err)
			}

			fmt.Println(prettyJSON(obj))
		},
	}
	commands[7] = &cobra.Command{
		Use:   "destroy [id]",
		Short: fmt.Sprintf("Destroy %v by id", singularName),
		Run: func(c *cobra.Command, args []string) {
			if len(args) != 1 {
				log.Fatalf("%v requires 1 argument\n", c.UseLine())
			}
			obj := maker()
			if crowbar.SetId(obj, args[0]) != nil {
				log.Fatalf("Failed to parse ID %v for an %v\n", args[0], singularName)
			}
			if err := crowbar.Destroy(obj); err != nil {
				log.Fatalf("Unable to destroy %v %v\nError: %v\n", singularName, args[0], err.Error())
			}
			fmt.Printf("Deleted %v %v\n", singularName, args[0])
		},
	}

	res.AddCommand(commands...)
	// Add relavent subcommands as needed.
	addAttriberCommands(singularName, maker, res)
	addDeploymenterCommands(singularName, maker, res)
	addDeploymentRolerCommands(singularName, maker, res)
	addNetworkerCommands(singularName, maker, res)
	addNetworkRangerCommands(singularName, maker, res)
	addNetworkAllocaterCommands(singularName, maker, res)
	addNoderCommands(singularName, maker, res)
	addRolerCommands(singularName, maker, res)
	addHammererCommands(singularName, maker, res)
	addJiggerCommands(singularName, maker, res)
	return res
}

func main() {
	app.PersistentPreRun = func(c *cobra.Command, a []string) {
		d("Talking to Crowbar with %v (%v:%v)", endpoint, username, password)
		if err := crowbar.Session(endpoint, username, password); err != nil {
			log.Fatalf("Could not connect to Crowbar: %v\n", err.Error())
		}
	}

	ping := &cobra.Command{
		Use:   "ping",
		Short: "Test to see if we can connect to the Crowbar API endpoint",
		Run: func(cmd *cobra.Command, args []string) {
			log.Printf("Able to connect to Crowbar at %v (user: %v)", endpoint, username)
		},
	}

	app.AddCommand(ping)

	app.Execute()
}
