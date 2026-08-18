package swan

type LoadConnRequest map[string]ConnConf

type ConnConf struct {
	Version     string               `vici:"version"`
	LocalAddrs  []string             `vici:"local_addrs"`
	RemoteAddrs []string             `vici:"remote_addrs"`
	KeyingTries string               `vici:"keyingtries,omitempty"`
	Local       AuthConf             `vici:"local"`
	Remote      AuthConf             `vici:"remote"`
	Children    map[string]ChildConf `vici:"children"`
}

type AuthConf struct {
	Auth    string   `vici:"auth"`
	Certs   []string `vici:"certs,omitempty"`
	CACerts []string `vici:"cacerts,omitempty"`
	ID      string   `vici:"id"`
}

type ChildConf struct {
	Mode        string   `vici:"mode"`
	LocalTS     []string `vici:"local_ts"`
	RemoteTS    []string `vici:"remote_ts"`
	IfIDIn      string   `vici:"if_id_in"`
	IfIDOut     string   `vici:"if_id_out"`
	StartAction string   `vici:"start_action"`
}

type LoadKeyRequest struct {
	Type string `vici:"type"`
	Data string `vici:"data"`
}
