// Automatically update the end point for the open-vpn client in RouterOS
// Handy in a crontab if your VPN is sitting on a dynamic ip

package main

import (
	"flag"
	"github.com/golang/glog"
	"github.com/simon-martin/routeros-go/api"
	"net"
)

func main() {
	// Defaults are same as RouterOS
	var host = flag.String("router-host", "192.168.88.1", "Hostname or IP of the router")
	var port = flag.Int("port", 8728, "Port to use")
	var user = flag.String("user", "admin", "User to authenticate with")
	var pass = flag.String("password", "", "Passwrod to authenticate with")
	var vpn_host = flag.String("vpn-host", "", "")
	flag.Parse()

	// Construct a client and connect
	client := routeros_api.Client{
		Host:     *host,
		Port:     *port,
		User:     *user,
		Password: *pass,
	}
	glog.Infoln("Checking VPN")
	err := client.Connect()
	if err != nil {
		glog.Fatalln("Error:", err)
	}

	// Close the connection when we're done
	defer client.Close()

	// Collect the current config - if the VPN is running then we're done
	reply := get_opvenvpn_config(client)
	if reply.Attributes["=running"] == "true" {
		glog.Exitln("VPN running to", reply.Attributes["=connect-to"])
	}

	// The VPN is not running - get the currently configured server IP
	// and compare to the given IP. If they match whatever is bust is
	// out of scope for this little script
	glog.Infoln("VPN not running to", reply.Attributes["=connect-to"])
	config_ip := net.ParseIP(reply.Attributes["=connect-to"])
	iface_id := reply.Attributes["=.id"]
	lookup_ip := get_ip(*vpn_host, false)

	if config_ip.Equal(lookup_ip) {
		glog.Exitln("IP is correct, can not fix")
	}

	// Otherwise, we may just be able to fix it!
	set_openvpn_ip(client, iface_id, lookup_ip)

}

// Collect the current open vpn client config
func get_opvenvpn_config(client routeros_api.Client) routeros_api.Sentence {
	query := routeros_api.Sentence{
		Command: "/interface/ovpn-client/print",
	}
	reply, err := client.RunCommand(query)
	if err != nil {
		glog.Fatalln("Error:", err)
	}

	return reply[0]
}

// Update the server IP
func set_openvpn_ip(client routeros_api.Client, iface_id string, ip net.IP) {
	glog.Infoln("Setting IP to", ip)
	query := routeros_api.Sentence{
		Command: "/interface/ovpn-client/set",
		Attributes: map[string]string{
			"=.id":        iface_id,
			"=connect-to": ip.String(),
		},
	}
	reply, err := client.RunCommand(query)
	if err != nil {
		glog.Fatalln("Error:", err)
	}
	glog.V(1).Infoln("Set reply:", reply)

}

// Resolve the IP
func get_ip(url string, prefer6 bool) net.IP {
	glog.Infoln("Looking up", url)
	var ipv4, ipv6 net.IP

	ips, err := net.LookupHost(url)
	if err != nil {
		glog.Fatalln("Error:", err)
	}

	for _, ip := range ips {
		glog.V(1).Infoln("Got IP:", ip)
		ip := net.ParseIP(ip)
		if ip.To4() == nil && ipv6 == nil {
			ipv6 = ip
			if prefer6 == true {
				break
			}
		} else {
			ipv4 = ip
			if prefer6 != true {
				break
			}

		}
	}

	if prefer6 {
		return ipv6
	}
	return ipv4
}
